package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Zweipanel-Dateimanager (Midnight-Commander-Stil) als Overlay über dem Remote-Bild.
// Links das lokale Dateisystem des Operators, rechts das des Geräts. Der Geräte-Zugriff
// läuft über dieselbe Command-Queue wie die Web-UI (viewer-files-API, Token-Auth).
// Alle Netz-/Dateioperationen laufen in Goroutinen und aktualisieren den Zustand unter
// fm.mu; die Render-Schleife zeichnet währenddessen einen „…"-Hinweis.

type fmEntry struct {
	name string
	path string
	dir  bool
	size int64
}

type fmPane struct {
	remote  bool
	path    string
	entries []fmEntry
	sel     int
	top     int
	err     string
	loading bool
}

// promptState hält eine modale Eingabe (Ordnername) bzw. eine Bestätigung (Löschen).
type promptState struct {
	kind  string // "" | "mkdir" | "delete"
	text  string // Eingabepuffer (mkdir) bzw. anzuzeigender Pfad (delete)
	label string
}

type fileManager struct {
	txt    *textRenderer
	rn     *sdl.Renderer
	cl     *fmClient
	active bool
	scale  float32 // UI-Skalierung (HiDPI); 1.0 = normal

	mu     sync.Mutex
	local  fmPane
	remote fmPane
	focus  int // 0 = lokal, 1 = Gerät
	status string
	busy   bool
	prompt promptState

	// Layout-Trefferzonen (gesetzt beim Zeichnen, gelesen bei Mausklick).
	colX  [2]float32
	colW  float32
	listY float32
	rowH  float32

	// Buttonleiste (gesetzt beim Zeichnen, gelesen bei Mausklick).
	btns       []fmButton
	btnY, btnH float32
}

// fmButton ist ein anklickbarer Knopf in der Aktionsleiste des Dateimanagers.
type fmButton struct {
	id, label string
	x, w      float32
}

func newFileManager(txt *textRenderer, cfg *launchConfig) *fileManager {
	fm := &fileManager{
		txt:   txt,
		rn:    txt.renderer,
		cl:    newFMClient(cfg),
		scale: 1,
	}
	fm.local.remote = false
	fm.remote.remote = true
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/"
	}
	fm.local.path = home
	fm.remote.path = "" // "" = Wurzeln (Laufwerke bzw. /) auf dem Gerät
	return fm
}

// toggle öffnet/schließt den Manager; beim ersten Öffnen werden beide Panels geladen.
func (fm *fileManager) toggle() {
	fm.active = !fm.active
	if !fm.active {
		return
	}
	fm.mu.Lock()
	needLocal := fm.local.entries == nil
	needRemote := fm.remote.entries == nil
	fm.mu.Unlock()
	if needLocal {
		fm.load(&fm.local, fm.local.path)
	}
	if needRemote {
		fm.load(&fm.remote, fm.remote.path)
	}
}

func (fm *fileManager) curPane() *fmPane {
	if fm.focus == 1 {
		return &fm.remote
	}
	return &fm.local
}

func (fm *fileManager) otherPane() *fmPane {
	if fm.focus == 1 {
		return &fm.local
	}
	return &fm.remote
}

// load füllt ein Panel (async). Für das lokale Panel per os.ReadDir, für das Gerät
// über die viewer-files-API.
func (fm *fileManager) load(p *fmPane, path string) {
	fm.mu.Lock()
	p.loading = true
	p.err = ""
	fm.mu.Unlock()
	go func() {
		var l fmListing
		if p.remote {
			l = fm.cl.browse(path)
		} else {
			l = localBrowse(path)
		}
		fm.mu.Lock()
		p.loading = false
		if l.err != "" {
			p.err = l.err
		} else {
			p.path = l.path
			p.entries = withParent(l)
			p.sel, p.top = 0, 0
		}
		fm.mu.Unlock()
	}()
}

// withParent stellt einen „../"-Eintrag voran (sofern ein Elternverzeichnis existiert).
func withParent(l fmListing) []fmEntry {
	out := make([]fmEntry, 0, len(l.entries)+1)
	if l.parent != "" || l.path != "" {
		out = append(out, fmEntry{name: "..", path: l.parent, dir: true})
	}
	out = append(out, l.entries...)
	return out
}

// --- Eingabe ---

// onKey verarbeitet einen Tastendruck. Liefert true, wenn er verbraucht wurde.
func (fm *fileManager) onKey(sym sdl.Keycode) bool {
	fm.mu.Lock()
	// Modale Eingabe zuerst.
	if fm.prompt.kind != "" {
		switch sym {
		case sdl.KeycodeEscape:
			fm.prompt = promptState{}
		case sdl.KeycodeReturn, sdl.KeycodeKpEnter:
			pr := fm.prompt
			fm.prompt = promptState{}
			fm.mu.Unlock()
			fm.commitPrompt(pr)
			return true
		case sdl.KeycodeBackspace:
			if fm.prompt.kind == "mkdir" && fm.prompt.text != "" {
				r := []rune(fm.prompt.text)
				fm.prompt.text = string(r[:len(r)-1])
			}
		}
		fm.mu.Unlock()
		return true
	}
	p := fm.curPane()
	n := len(p.entries)
	switch sym {
	case sdl.KeycodeEscape:
		fm.mu.Unlock()
		fm.active = false
		return true
	case sdl.KeycodeTab:
		fm.focus ^= 1
	case sdl.KeycodeUp:
		if p.sel > 0 {
			p.sel--
		}
	case sdl.KeycodeDown:
		if p.sel < n-1 {
			p.sel++
		}
	case sdl.KeycodePageUp:
		p.sel -= 12
		if p.sel < 0 {
			p.sel = 0
		}
	case sdl.KeycodePageDown:
		p.sel += 12
		if p.sel > n-1 {
			p.sel = n - 1
		}
	case sdl.KeycodeHome:
		p.sel = 0
	case sdl.KeycodeEnd:
		p.sel = n - 1
	case sdl.KeycodeReturn, sdl.KeycodeKpEnter, sdl.KeycodeRight:
		fm.mu.Unlock()
		fm.enter(p)
		return true
	case sdl.KeycodeBackspace, sdl.KeycodeLeft:
		fm.mu.Unlock()
		fm.goParent(p)
		return true
	case sdl.KeycodeF5:
		fm.mu.Unlock()
		fm.transfer()
		return true
	case sdl.KeycodeF7:
		fm.prompt = promptState{kind: "mkdir", label: "Neuer Ordner in " + paneLabel(p) + ":"}
	case sdl.KeycodeF8, sdl.KeycodeDelete:
		if n > 0 && p.entries[p.sel].name != ".." {
			e := p.entries[p.sel]
			fm.prompt = promptState{kind: "delete", text: e.path, label: "Löschen: " + e.name + "  (Enter=ja, Esc=nein)"}
		}
	default:
		fm.mu.Unlock()
		return false
	}
	fm.mu.Unlock()
	return true
}

// onText nimmt getippte Zeichen für die modale Eingabe (Ordnername) entgegen.
func (fm *fileManager) onText(s string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.prompt.kind == "mkdir" {
		fm.prompt.text += s
	}
}

// enter öffnet das ausgewählte Verzeichnis (oder ../).
func (fm *fileManager) enter(p *fmPane) {
	fm.mu.Lock()
	if len(p.entries) == 0 {
		fm.mu.Unlock()
		return
	}
	e := p.entries[p.sel]
	fm.mu.Unlock()
	if !e.dir {
		return
	}
	if e.name == ".." {
		fm.goParent(p)
		return
	}
	fm.load(p, e.path)
}

func (fm *fileManager) goParent(p *fmPane) {
	fm.mu.Lock()
	entries := p.entries
	fm.mu.Unlock()
	if len(entries) > 0 && entries[0].name == ".." {
		fm.load(p, entries[0].path)
	}
}

// transfer kopiert die Auswahl ins andere Panel (F5) – Datei oder rekursiv einen
// ganzen Ordner. Läuft asynchron; Fortschritt/Ergebnis stehen in der Fußzeile.
func (fm *fileManager) transfer() {
	fm.mu.Lock()
	if fm.busy {
		fm.mu.Unlock()
		return
	}
	src := fm.curPane()
	dst := fm.otherPane()
	if len(src.entries) == 0 {
		fm.mu.Unlock()
		return
	}
	e := src.entries[src.sel]
	dstPath := dst.path
	dstRemote := dst.remote
	srcRemote := src.remote
	fm.mu.Unlock()

	if e.name == ".." {
		return
	}
	if srcRemote == dstRemote {
		fm.setStatus("Quelle und Ziel sind dieselbe Seite")
		return
	}
	fm.mu.Lock()
	fm.busy = true
	fm.mu.Unlock()
	go func() {
		defer func() { fm.mu.Lock(); fm.busy = false; fm.mu.Unlock() }()
		var count int
		if e.dir {
			fm.setStatus("Ordner-Transfer: " + e.name + " …")
		} else {
			fm.setStatus("Übertrage " + e.name + " …")
		}
		if err := fm.copyTree(e, dstPath, srcRemote, dstRemote, &count); err != nil {
			fm.setStatus("Fehler: " + err.Error())
			fm.load(dst, dstPath) // teils Übertragenes sichtbar machen
			return
		}
		if e.dir {
			fm.setStatus(fmt.Sprintf("✓ %s übertragen (%d Dateien)", e.name, count))
		} else {
			fm.setStatus("✓ " + e.name + " übertragen")
		}
		fm.load(dst, dstPath) // Zielpanel aktualisieren
	}()
}

// copyTree kopiert src (Datei oder Ordner) rekursiv in das Zielverzeichnis dstDir.
// srcRemote/dstRemote geben die Seiten an. count zählt tatsächlich kopierte Dateien.
func (fm *fileManager) copyTree(src fmEntry, dstDir string, srcRemote, dstRemote bool, count *int) error {
	if !src.dir {
		return fm.copyFile(src, dstDir, srcRemote, dstRemote, count)
	}
	target := joinFor(dstDir, src.name, dstRemote)
	if err := fm.mkdirFor(target, dstRemote); err != nil {
		return fmt.Errorf("Ordner %s: %s", src.name, err)
	}
	var l fmListing
	if srcRemote {
		l = fm.cl.browse(src.path)
	} else {
		l = localBrowse(src.path)
	}
	if l.err != "" {
		return fmt.Errorf("%s: %s", src.name, l.err)
	}
	for _, c := range l.entries {
		if c.name == "." || c.name == ".." {
			continue
		}
		if err := fm.copyTree(c, target, srcRemote, dstRemote, count); err != nil {
			return err
		}
	}
	return nil
}

// copyFile überträgt eine einzelne Datei zwischen den Seiten.
func (fm *fileManager) copyFile(e fmEntry, dstDir string, srcRemote, dstRemote bool, count *int) error {
	var data []byte
	var err error
	if srcRemote {
		data, err = fm.cl.read(e.path)
	} else {
		data, err = os.ReadFile(e.path)
	}
	if err != nil {
		return fmt.Errorf("%s: %s", e.name, err)
	}
	if int64(len(data)) > maxViewerTransfer {
		return fmt.Errorf("%s zu groß (max 32 MB)", e.name)
	}
	target := joinFor(dstDir, e.name, dstRemote)
	if dstRemote {
		err = fm.cl.write(target, data)
	} else {
		err = os.WriteFile(target, data, 0644)
	}
	if err != nil {
		return fmt.Errorf("%s: %s", e.name, err)
	}
	*count++
	fm.setStatus(fmt.Sprintf("kopiere … %s  (%d)", e.name, *count))
	return nil
}

// joinFor verbindet Verzeichnis + Name für die jeweilige Seite (Remote: geräte-
// gerechtes Trennzeichen; lokal: filepath.Join).
func joinFor(dir, name string, remote bool) string {
	if remote {
		return remoteJoin(dir, name)
	}
	return filepath.Join(dir, name)
}

// mkdirFor legt ein Verzeichnis auf der jeweiligen Seite an (idempotent: MkdirAll
// bzw. agentseitig ebenfalls MkdirAll – bestehende Ordner sind kein Fehler).
func (fm *fileManager) mkdirFor(path string, remote bool) error {
	if remote {
		return fm.cl.mkdir(path)
	}
	return os.MkdirAll(path, 0755)
}

// commitPrompt führt die bestätigte modale Aktion aus (Ordner anlegen / löschen).
func (fm *fileManager) commitPrompt(pr promptState) {
	switch pr.kind {
	case "mkdir":
		name := strings.TrimSpace(pr.text)
		if name == "" {
			return
		}
		fm.mu.Lock()
		p := fm.curPane()
		base := p.path
		remote := p.remote
		fm.mu.Unlock()
		go func() {
			var err error
			if remote {
				err = fm.cl.mkdir(remoteJoin(base, name))
			} else {
				err = os.Mkdir(filepath.Join(base, name), 0755)
			}
			if err != nil {
				fm.setStatus("Fehler: " + err.Error())
				return
			}
			fm.setStatus("✓ Ordner angelegt: " + name)
			fm.load(p, base)
		}()
	case "delete":
		fm.mu.Lock()
		p := fm.curPane()
		base := p.path
		remote := p.remote
		fm.mu.Unlock()
		go func() {
			var err error
			if remote {
				err = fm.cl.delete(pr.text)
			} else {
				err = os.RemoveAll(pr.text)
			}
			if err != nil {
				fm.setStatus("Fehler: " + err.Error())
				return
			}
			fm.setStatus("✓ gelöscht")
			fm.load(p, base)
		}()
	}
}

func (fm *fileManager) setStatus(s string) {
	fm.mu.Lock()
	fm.status = s
	fm.mu.Unlock()
}

// doButton führt eine über die Buttonleiste ausgelöste Aktion aus (ohne gehaltenes
// fm.mu – die einzelnen Helfer sperren selbst).
func (fm *fileManager) doButton(id string) {
	switch id {
	case "copy":
		fm.transfer()
	case "mkdir":
		fm.startMkdir()
	case "delete":
		fm.startDelete()
	case "refresh":
		fm.refresh()
	case "close":
		fm.active = false
	}
}

// startMkdir öffnet die modale Ordner-Eingabe für das fokussierte Panel.
func (fm *fileManager) startMkdir() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	p := fm.curPane()
	fm.prompt = promptState{kind: "mkdir", label: "Neuer Ordner in " + paneLabel(p) + ":"}
}

// startDelete öffnet die Löschbestätigung für den ausgewählten Eintrag.
func (fm *fileManager) startDelete() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	p := fm.curPane()
	if len(p.entries) == 0 || p.entries[p.sel].name == ".." {
		return
	}
	e := p.entries[p.sel]
	fm.prompt = promptState{kind: "delete", text: e.path, label: "Löschen: " + e.name + "  (Enter=ja, Esc=nein)"}
}

// refresh lädt das fokussierte Panel neu.
func (fm *fileManager) refresh() {
	fm.mu.Lock()
	p := fm.curPane()
	base := p.path
	fm.mu.Unlock()
	fm.load(p, base)
}

// --- Maus ---

// onClick verarbeitet einen Linksklick (Fensterkoordinaten). Liefert true, wenn er
// im Manager lag.
func (fm *fileManager) onClick(mx, my float32, double bool) bool {
	fm.mu.Lock()
	// Buttonleiste (unter den Listen, über der Fußzeile) zuerst prüfen.
	if fm.btnH > 0 && my >= fm.btnY && my < fm.btnY+fm.btnH {
		id := ""
		for _, b := range fm.btns {
			if mx >= b.x && mx < b.x+b.w {
				id = b.id
				break
			}
		}
		fm.mu.Unlock()
		if id != "" {
			fm.doButton(id)
		}
		return true
	}
	if my < fm.listY || fm.rowH <= 0 {
		fm.mu.Unlock()
		return false
	}
	col := -1
	for i := 0; i < 2; i++ {
		if mx >= fm.colX[i] && mx < fm.colX[i]+fm.colW {
			col = i
		}
	}
	if col < 0 {
		fm.mu.Unlock()
		return true // Klick im Overlay, aber neben den Listen
	}
	fm.focus = col
	p := fm.curPane()
	idx := p.top + int((my-fm.listY)/fm.rowH)
	if idx >= 0 && idx < len(p.entries) {
		p.sel = idx
		fm.mu.Unlock()
		if double {
			fm.enter(p)
		}
		return true
	}
	fm.mu.Unlock()
	return true
}

// onWheel scrollt das fokussierte Panel.
func (fm *fileManager) onWheel(dy float32) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	p := fm.curPane()
	step := 3
	if dy > 0 {
		p.sel -= step
	} else {
		p.sel += step
	}
	if p.sel < 0 {
		p.sel = 0
	}
	if p.sel > len(p.entries)-1 {
		p.sel = len(p.entries) - 1
	}
}

// --- HTTP-Client für die viewer-files-API ---

const maxViewerTransfer = 32 << 20

type fmListing struct {
	path    string
	parent  string
	entries []fmEntry
	err     string
}

type fmClient struct {
	http   *http.Client
	base   string // https://host
	device string
	token  string
}

func newFMClient(cfg *launchConfig) *fmClient {
	c := &fmClient{http: newHTTPClient(cfg.Insecure), base: httpBase(cfg.URL), device: cfg.Device, token: cfg.Token}
	return c
}

func (c *fmClient) url(p string) string {
	return c.base + "/api/v1/devices/" + c.device + "/viewer-files" + p
}

func (c *fmClient) post(path string, body any) (string, error) {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, c.url(path), bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("server: %s", resp.Status)
	}
	var r struct {
		CommandID string `json:"command_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.CommandID, nil
}

// poll wartet auf das Ergebnis eines eingereihten Befehls.
func (c *fmClient) poll(id string) (string, int, error) {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, c.url("/command/"+id), nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			return "", 0, err
		}
		var cmd struct {
			Status   string `json:"status"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&cmd)
		resp.Body.Close()
		if cmd.Status == "done" {
			return cmd.Output, cmd.ExitCode, nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return "", 0, fmt.Errorf("Zeitüberschreitung – Gerät antwortet nicht")
}

func (c *fmClient) browse(path string) fmListing {
	id, err := c.post("/browse", map[string]string{"path": path})
	if err != nil {
		return fmListing{err: err.Error()}
	}
	out, _, err := c.poll(id)
	if err != nil {
		return fmListing{err: err.Error()}
	}
	var raw struct {
		Path    string `json:"path"`
		Parent  string `json:"parent"`
		Error   string `json:"error"`
		Entries []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Dir  bool   `json:"dir"`
			Size int64  `json:"size"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return fmListing{err: "ungültige Antwort"}
	}
	if raw.Error != "" {
		return fmListing{path: path, err: raw.Error}
	}
	l := fmListing{path: raw.Path, parent: raw.Parent}
	for _, e := range raw.Entries {
		l.entries = append(l.entries, fmEntry{name: e.Name, path: e.Path, dir: e.Dir, size: e.Size})
	}
	return l
}

func (c *fmClient) read(path string) ([]byte, error) {
	id, err := c.post("/read", map[string]string{"path": path})
	if err != nil {
		return nil, err
	}
	out, exit, err := c.poll(id)
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, fmt.Errorf("%s", out)
	}
	req, _ := http.NewRequest(http.MethodGet, c.url("/blob/"+id), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxViewerTransfer+1))
}

func (c *fmClient) write(path string, data []byte) error {
	req, _ := http.NewRequest(http.MethodPost, c.url("/write")+"?path="+url.QueryEscape(path), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	var r struct {
		CommandID string `json:"command_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	resp.Body.Close()
	if resp.StatusCode >= 300 || r.CommandID == "" {
		return fmt.Errorf("upload: %s", resp.Status)
	}
	out, exit, err := c.poll(r.CommandID)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func (c *fmClient) mkdir(path string) error {
	id, err := c.post("/mkdir", map[string]string{"path": path})
	if err != nil {
		return err
	}
	out, exit, err := c.poll(id)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func (c *fmClient) delete(path string) error {
	id, err := c.post("/delete", map[string]string{"path": path})
	if err != nil {
		return err
	}
	out, exit, err := c.poll(id)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// --- lokales Dateisystem ---

func localBrowse(path string) fmListing {
	if path == "" {
		path = "/"
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	des, err := os.ReadDir(path)
	if err != nil {
		return fmListing{path: path, parent: parent, err: err.Error()}
	}
	l := fmListing{path: path, parent: parent}
	for _, d := range des {
		info, ierr := d.Info()
		fe := fmEntry{name: d.Name(), path: filepath.Join(path, d.Name()), dir: d.IsDir()}
		if ierr == nil {
			fe.size = info.Size()
		}
		l.entries = append(l.entries, fe)
	}
	sort.Slice(l.entries, func(i, j int) bool {
		if l.entries[i].dir != l.entries[j].dir {
			return l.entries[i].dir
		}
		return strings.ToLower(l.entries[i].name) < strings.ToLower(l.entries[j].name)
	})
	return l
}

// --- Helfer ---

// remoteJoin verbindet ein Geräte-Verzeichnis mit einem Namen unter Beibehaltung des
// erkannten Trennzeichens (Windows „\", sonst „/").
func remoteJoin(dir, name string) string {
	if dir == "" {
		return name
	}
	sep := "/"
	if strings.Contains(dir, "\\") || (len(dir) >= 2 && dir[1] == ':') {
		sep = "\\"
	}
	return strings.TrimRight(dir, "/\\") + sep + name
}

func paneLabel(p *fmPane) string {
	if p.remote {
		return "Gerät"
	}
	return "lokal"
}

func newHTTPClient(insecure bool) *http.Client {
	c := &http.Client{Timeout: 90 * time.Second}
	if insecure {
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return c
}

// httpBase liefert die http(s)-Basis-URL aus der (evtl. ws/wss-)Server-URL.
func httpBase(u string) string {
	switch {
	case strings.HasPrefix(u, "wss://"):
		return "https://" + strings.TrimPrefix(u, "wss://")
	case strings.HasPrefix(u, "ws://"):
		return "http://" + strings.TrimPrefix(u, "ws://")
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		return u
	default:
		return "https://" + u
	}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dK", n/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
