//go:build linux

package remote

import (
	"encoding/binary"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
)

// x11Clipboard bedient die CLIPBOARD-Auswahl: Besitzwechsel kommen per XFixes, der Inhalt
// wird als UTF8_STRING abgeholt; nach SetClipboard besitzt ein unsichtbares Fenster die
// Auswahl und beantwortet Anfragen anderer Programme (TARGETS/UTF8_STRING/STRING/TEXT).
type x11Clipboard struct {
	c                                   *xgb.Conn
	win                                 xproto.Window
	clipboard, utf8, targets, str, text xproto.Atom
	prop, atom                          xproto.Atom
	mu                                  sync.Mutex
	owned                               string
	owning                              bool
	last, pending                       string
	changed, seen                       bool
	done                                chan struct{}
}

const x11ClipMax = 1 << 20

func newX11Clipboard(c *xgb.Conn) (*x11Clipboard, error) {
	if err := xfixes.Init(c); err != nil {
		return nil, err
	}
	if _, err := xfixes.QueryVersion(c, 4, 0).Reply(); err != nil {
		return nil, err
	}
	cb := &x11Clipboard{c: c, done: make(chan struct{})}
	for name, dst := range map[string]*xproto.Atom{
		"CLIPBOARD": &cb.clipboard, "UTF8_STRING": &cb.utf8, "TARGETS": &cb.targets,
		"STRING": &cb.str, "TEXT": &cb.text, "ROSTER_CLIP": &cb.prop, "ATOM": &cb.atom,
	} {
		r, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
		if err != nil {
			return nil, err
		}
		*dst = r.Atom
	}
	scr := xproto.Setup(c).DefaultScreen(c)
	win, err := xproto.NewWindowId(c)
	if err != nil {
		return nil, err
	}
	cb.win = win
	if err := xproto.CreateWindowChecked(c, 0, win, scr.Root, -10, -10, 1, 1, 0,
		xproto.WindowClassInputOnly, scr.RootVisual, 0, nil).Check(); err != nil {
		return nil, err
	}
	xfixes.SelectSelectionInput(c, win, cb.clipboard, xfixes.SelectionEventMaskSetSelectionOwner)
	go cb.loop()
	cb.request() // aktuellen Inhalt einmal abholen
	return cb, nil
}

func (cb *x11Clipboard) request() {
	xproto.ConvertSelection(cb.c, cb.win, cb.clipboard, cb.utf8, cb.prop, xproto.TimeCurrentTime)
}

func (cb *x11Clipboard) loop() {
	defer close(cb.done)
	for {
		ev, err := cb.c.WaitForEvent()
		if ev == nil && err == nil {
			return // Verbindung zu
		}
		switch e := ev.(type) {
		case xfixes.SelectionNotifyEvent:
			if e.Owner != cb.win && e.Owner != 0 {
				cb.request()
			}
		case xproto.SelectionNotifyEvent:
			if e.Property == 0 {
				continue // Besitzer kann kein UTF8_STRING
			}
			r, err := xproto.GetProperty(cb.c, true, cb.win, cb.prop, xproto.GetPropertyTypeAny, 0, x11ClipMax/4).Reply()
			if err != nil || (r.Type != cb.utf8 && r.Type != cb.str) {
				continue // u.a. INCR (sehr große Inhalte) wird nicht unterstützt
			}
			text := string(r.Value)
			if r.Type == cb.str {
				text = latin1Decode(r.Value)
			}
			cb.mu.Lock()
			if !cb.seen || text != cb.last {
				cb.last, cb.pending, cb.changed, cb.seen = text, text, true, true
			}
			cb.mu.Unlock()
		case xproto.SelectionRequestEvent:
			cb.serve(e)
		case xproto.SelectionClearEvent:
			cb.mu.Lock()
			cb.owning = false
			cb.mu.Unlock()
		}
	}
}

// serve beantwortet die Anfrage eines anderen Programms nach unserem Inhalt.
func (cb *x11Clipboard) serve(e xproto.SelectionRequestEvent) {
	cb.mu.Lock()
	text, owning := cb.owned, cb.owning
	cb.mu.Unlock()
	prop := e.Property
	if prop == 0 {
		prop = e.Target // veraltete Clients
	}
	switch {
	case !owning || e.Selection != cb.clipboard:
		prop = 0
	case e.Target == cb.targets:
		data := make([]byte, 0, 16) // xgb meldet sich als little-endian-Client an
		for _, a := range []xproto.Atom{cb.targets, cb.utf8, cb.str, cb.text} {
			data = binary.LittleEndian.AppendUint32(data, uint32(a))
		}
		xproto.ChangeProperty(cb.c, xproto.PropModeReplace, e.Requestor, prop, cb.atom, 32, 4, data)
	case e.Target == cb.utf8 || e.Target == cb.text:
		xproto.ChangeProperty(cb.c, xproto.PropModeReplace, e.Requestor, prop, cb.utf8, 8, uint32(len(text)), []byte(text))
	case e.Target == cb.str:
		b := latin1Encode(text)
		xproto.ChangeProperty(cb.c, xproto.PropModeReplace, e.Requestor, prop, cb.str, 8, uint32(len(b)), b)
	default:
		prop = 0
	}
	n := xproto.SelectionNotifyEvent{Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target, Property: prop}
	xproto.SendEvent(cb.c, false, e.Requestor, 0, string(n.Bytes()))
	cb.c.Sync()
}

func (cb *x11Clipboard) get() (string, bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if !cb.changed {
		return "", false
	}
	cb.changed = false
	return cb.pending, true
}

func (cb *x11Clipboard) set(text string) {
	if len(text) > 256<<10 {
		text = text[:256<<10]
	}
	cb.mu.Lock()
	cb.owned, cb.owning, cb.last, cb.seen, cb.changed = text, true, text, true, false
	cb.mu.Unlock()
	xproto.SetSelectionOwner(cb.c, cb.win, cb.clipboard, xproto.TimeCurrentTime)
	cb.c.Sync()
}

func (cb *x11Clipboard) close() {
	cb.c.Close()
	<-cb.done
}
