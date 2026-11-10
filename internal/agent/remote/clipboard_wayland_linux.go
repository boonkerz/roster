//go:build linux

package remote

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Zwischenablage unter Wayland über ext-data-control (bzw. älter wlr-data-control): beide
// Protokolle haben dieselben Opcodes und erlauben einem Client ohne Fenster/Fokus, die
// Auswahl zu lesen und zu setzen.

var wlTextMimes = []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "STRING", "TEXT"}

const wlClipMax = 1 << 20

type wlClipboard struct {
	c       *wlConn
	mgr     uint32
	dev     uint32
	offers  map[uint32][]string // Angebot -> MIME-Typen
	sel     uint32              // aktuelles Auswahl-Angebot
	pending uint32              // noch nicht gelesenes Angebot
	src     uint32              // eigene Quelle (wir besitzen die Auswahl)
	srcText string
	last    string // zuletzt gemeldet/gesetzt (gegen Echos)
	seen    bool
}

func (cb *wlClipboard) setup(c *wlConn, reg, seat uint32) {
	g := c.find("ext_data_control_manager_v1")
	if len(g) == 0 {
		g = c.find("zwlr_data_control_manager_v1")
	}
	if len(g) == 0 {
		return
	}
	mgr, _, err := c.bind(reg, g[0], 1)
	if err != nil {
		return
	}
	cb.c, cb.mgr, cb.offers = c, mgr, map[uint32][]string{}
	cb.dev = c.newID()
	_ = c.send(mgr, 1, (&wlArgs{}).u32(cb.dev).u32(seat)) // get_data_device
}

func (cb *wlClipboard) active() bool { return cb.c != nil && cb.dev != 0 }

// handle verarbeitet Events der Zwischenablage-Objekte; andere werden ignoriert.
func (s *waylandSource) handle(ev wlEvent) {
	cb := &s.clip
	if !cb.active() && cb.src == 0 {
		return
	}
	r := &wlReader{b: ev.data}
	switch {
	case ev.obj == cb.dev:
		switch ev.opcode {
		case 0: // data_offer(new id)
			cb.offers[r.u32()] = nil
		case 1: // selection(offer|null)
			id := r.u32()
			if cb.sel != 0 && cb.sel != id {
				cb.destroyOffer(cb.sel)
			}
			cb.sel = id
			if id != 0 && cb.src == 0 {
				cb.pending = id
			} else {
				cb.pending = 0
			}
		case 2: // finished
			cb.dev = 0
		case 3: // primary_selection – nicht genutzt
			if id := r.u32(); id != 0 && id != cb.sel {
				cb.destroyOffer(id)
			}
		}
	case cb.offers != nil && ev.opcode == 0 && cb.isOffer(ev.obj):
		cb.offers[ev.obj] = append(cb.offers[ev.obj], r.str())
	case ev.obj == cb.src && cb.src != 0:
		switch ev.opcode {
		case 0: // send(mime, fd): Text in den FD schreiben
			_ = r.str()
			if fd := cb.c.takeFD(); fd >= 0 {
				text := cb.srcText
				go func() {
					f := os.NewFile(uintptr(fd), "clip")
					_, _ = f.WriteString(text)
					f.Close()
				}()
			}
		case 1: // cancelled: jemand anderes hat kopiert
			_ = cb.c.send(cb.src, 1, nil)
			cb.src = 0
		}
	}
}

func (cb *wlClipboard) isOffer(id uint32) bool { _, ok := cb.offers[id]; return ok }

func (cb *wlClipboard) destroyOffer(id uint32) {
	if cb.isOffer(id) {
		_ = cb.c.send(id, 1, nil)
		delete(cb.offers, id)
	}
}

// pickMime wählt den besten Text-Typ eines Angebots (rein, getestet).
func pickMime(mimes []string) string {
	for _, want := range wlTextMimes {
		for _, m := range mimes {
			if m == want {
				return m
			}
		}
	}
	return ""
}

// receive liest den Inhalt eines Angebots über eine Pipe (der Besitzer schreibt hinein).
func (cb *wlClipboard) receive(offer uint32) (string, error) {
	mime := pickMime(cb.offers[offer])
	if mime == "" {
		return "", errors.New("kein Text")
	}
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return "", err
	}
	err := cb.c.send(offer, 0, (&wlArgs{}).str(mime).fd(p[1]))
	unix.Close(p[1])
	rf := os.NewFile(uintptr(p[0]), "clip")
	defer rf.Close()
	if err != nil {
		return "", err
	}
	_ = rf.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 32<<10)
	for len(buf) < wlClipMax {
		n, err := rf.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break // EOF oder Zeitüberschreitung: nehmen, was da ist
		}
	}
	return string(buf), nil
}

// pump verarbeitet anstehende Events ohne zu warten.
func (s *waylandSource) pump() {
	for {
		// Abgelaufene Deadline liest Go gar nicht erst – daher eine Millisekunde.
		ev, err := s.c.read(time.Now().Add(time.Millisecond))
		if err != nil {
			return
		}
		if s.c.dispatchCore(ev) != nil {
			return
		}
		s.handle(ev)
	}
}

func (s *waylandSource) GetClipboard() (string, bool) {
	cb := &s.clip
	if !cb.active() {
		return "", false
	}
	s.pump()
	if cb.pending == 0 {
		return "", false
	}
	offer := cb.pending
	cb.pending = 0
	text, err := cb.receive(offer)
	if err != nil || (cb.seen && text == cb.last) {
		return "", false
	}
	cb.last, cb.seen = text, true
	return text, true
}

func (s *waylandSource) SetClipboard(text string) {
	cb := &s.clip
	if !cb.active() {
		return
	}
	if cb.src != 0 {
		_ = cb.c.send(cb.src, 1, nil)
	}
	cb.src = cb.c.newID()
	cb.srcText = text
	_ = cb.c.send(cb.mgr, 0, (&wlArgs{}).u32(cb.src)) // create_data_source
	for _, m := range wlTextMimes {
		_ = cb.c.send(cb.src, 0, (&wlArgs{}).str(m))
	}
	_ = cb.c.send(cb.dev, 0, (&wlArgs{}).u32(cb.src)) // set_selection
	cb.last, cb.seen, cb.pending = text, true, 0
}

func (cb *wlClipboard) close() {
	if cb.c == nil {
		return
	}
	for id := range cb.offers {
		_ = cb.c.send(id, 1, nil)
	}
	if cb.src != 0 {
		_ = cb.c.send(cb.src, 1, nil)
	}
	if cb.dev != 0 {
		_ = cb.c.send(cb.dev, 1, nil)
	}
	cb.c = nil
}
