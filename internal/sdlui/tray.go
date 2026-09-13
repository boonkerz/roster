package sdlui

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/jupiterrider/purego-sdl3/sdl"
)

// SDL3 kann seit 3.2 ein System-Tray-Icon anlegen (SDL_Tray, unter Linux über
// StatusNotifier/AppIndicator, unter Windows/macOS nativ). Das purego-Binding hat
// diese Funktionen noch auskommentiert, daher registrieren wir sie hier selbst –
// dieselbe geladene Bibliothek, nur zusätzliche Symbole. Fehlt das Symbol (ältere
// SDL3-Version), meldet TrayAvailable false und die App läuft ohne Tray weiter.

const (
	trayEntryButton uint32 = 0x00000001
	// trayEntryCheckbox / trayEntrySubmenu werden hier (noch) nicht gebraucht.
)

var (
	trayOnce sync.Once
	trayErr  error

	fnClickTrayEntry       func(entry uintptr)
	fnCreateTray           func(icon uintptr, tooltip string) uintptr
	fnCreateTrayMenu       func(tray uintptr) uintptr
	fnInsertTrayEntryAt    func(menu uintptr, pos int32, label string, flags uint32) uintptr
	fnInsertTraySeparator  func(menu uintptr, pos int32, label uintptr, flags uint32) uintptr
	fnSetTrayEntryCallback func(entry uintptr, callback uintptr, userdata uintptr)
	fnSetTrayEntryLabel    func(entry uintptr, label string)
	fnSetTrayEntryEnabled  func(entry uintptr, enabled bool)
	fnSetTrayTooltip       func(tray uintptr, tooltip string)
	fnSetTrayIcon          func(tray uintptr, icon uintptr)
	fnDestroyTray          func(tray uintptr)
	fnUpdateTrays          func()
)

func sdlLibName() string {
	switch runtime.GOOS {
	case "windows":
		return "SDL3.dll"
	case "darwin":
		return "libSDL3.dylib"
	default:
		return "libSDL3.so.0"
	}
}

func loadTray() error {
	trayOnce.Do(func() {
		lib, err := dlopenSDL(sdlLibName())
		if err != nil {
			trayErr = err
			return
		}
		reg := func(fptr any, name string) {
			if trayErr != nil {
				return
			}
			addr, err := dlsymSDL(lib, name)
			if err != nil || addr == 0 {
				trayErr = errors.New("SDL3 ohne Tray-Unterstützung (" + name + " fehlt)")
				return
			}
			purego.RegisterFunc(fptr, addr)
		}
		reg(&fnClickTrayEntry, "SDL_ClickTrayEntry")
		reg(&fnCreateTray, "SDL_CreateTray")
		reg(&fnCreateTrayMenu, "SDL_CreateTrayMenu")
		reg(&fnInsertTrayEntryAt, "SDL_InsertTrayEntryAt")
		reg(&fnInsertTraySeparator, "SDL_InsertTrayEntryAt")
		reg(&fnSetTrayEntryCallback, "SDL_SetTrayEntryCallback")
		reg(&fnSetTrayEntryLabel, "SDL_SetTrayEntryLabel")
		reg(&fnSetTrayEntryEnabled, "SDL_SetTrayEntryEnabled")
		reg(&fnSetTrayTooltip, "SDL_SetTrayTooltip")
		reg(&fnSetTrayIcon, "SDL_SetTrayIcon")
		reg(&fnDestroyTray, "SDL_DestroyTray")
		reg(&fnUpdateTrays, "SDL_UpdateTrays")
	})
	return trayErr
}

// TrayAvailable meldet, ob die laufende SDL3-Bibliothek die Tray-API mitbringt.
func TrayAvailable() bool { return loadTray() == nil }

// Tray ist ein Symbol im System-Tray samt Kontextmenü.
type Tray struct {
	handle uintptr
	menu   uintptr
	items  int32
	// keep hält die erzeugten C-Callbacks am Leben (purego gibt sie ohnehin nie
	// frei, aber so ist die Verbindung Eintrag→Aktion explizit dokumentiert).
	keep []func()
}

// TrayItem ist ein Menüeintrag, dessen Beschriftung sich später ändern lässt.
type TrayItem struct{ handle uintptr }

// NewTray legt das Tray-Symbol mit Tooltip an. icon darf nil sein (dann zeigt das
// System sein Standardsymbol). Der Aufruf muss auf dem SDL-Hauptthread erfolgen.
func NewTray(icon *sdl.Surface, tooltip string) (*Tray, error) {
	if err := loadTray(); err != nil {
		return nil, err
	}
	h := fnCreateTray(uintptr(unsafe.Pointer(icon)), tooltip)
	if h == 0 {
		return nil, errors.New("tray konnte nicht angelegt werden: " + sdl.GetError())
	}
	menu := fnCreateTrayMenu(h)
	if menu == 0 {
		fnDestroyTray(h)
		return nil, errors.New("tray-menü konnte nicht angelegt werden: " + sdl.GetError())
	}
	return &Tray{handle: h, menu: menu}, nil
}

// AddItem hängt einen Menüpunkt an. action läuft auf dem SDL-Hauptthread (aus der
// Event-Schleife heraus) – dort also nur kurz arbeiten oder ein Signal setzen.
func (t *Tray) AddItem(label string, action func()) *TrayItem {
	e := fnInsertTrayEntryAt(t.menu, t.items, label, trayEntryButton)
	t.items++
	if e == 0 {
		return &TrayItem{}
	}
	if action != nil {
		t.keep = append(t.keep, action)
		cb := purego.NewCallback(func(userdata uintptr, entry uintptr) { action() })
		fnSetTrayEntryCallback(e, cb, 0)
	}
	return &TrayItem{handle: e}
}

// AddSeparator hängt eine Trennlinie an (Eintrag ohne Beschriftung).
func (t *Tray) AddSeparator() {
	fnInsertTraySeparator(t.menu, t.items, 0, 0)
	t.items++
}

func (t *Tray) SetTooltip(s string) { fnSetTrayTooltip(t.handle, s) }

// SetIcon tauscht das Symbol (z. B. Farbwechsel bei Fehlern). Die übergebene
// Surface muss bis zum nächsten Wechsel gültig bleiben.
func (t *Tray) SetIcon(icon *sdl.Surface) { fnSetTrayIcon(t.handle, uintptr(unsafe.Pointer(icon))) }

func (t *Tray) Destroy() {
	if t.handle != 0 {
		fnDestroyTray(t.handle)
		t.handle, t.menu = 0, 0
	}
}

func (i *TrayItem) SetLabel(s string) {
	if i.handle != 0 {
		fnSetTrayEntryLabel(i.handle, s)
	}
}

// Click löst den Eintrag programmatisch aus – für Selbsttests der Callback-Kette.
func (i *TrayItem) Click() {
	if i.handle != 0 {
		fnClickTrayEntry(i.handle)
	}
}

func (i *TrayItem) SetEnabled(v bool) {
	if i.handle != 0 {
		fnSetTrayEntryEnabled(i.handle, v)
	}
}

// UpdateTrays pumpt die Tray-Ereignisse. SDL_PumpEvents erledigt das mit, schadet
// aber nicht – in Schleifen ohne Event-Pumpe ist es nötig.
func UpdateTrays() {
	if loadTray() == nil {
		fnUpdateTrays()
	}
}
