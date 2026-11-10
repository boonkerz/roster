//go:build linux

package remote

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// Minimaler Wayland-Client (Wire-Protokoll) ohne cgo – genug für wlr-screencopy,
// wlr-virtual-pointer und zwp-virtual-keyboard. Requests werden synchron geschrieben,
// Events in einer Schleife gelesen, bis eine Bedingung erfüllt ist.

type wlEvent struct {
	obj    uint32
	opcode uint16
	data   []byte
}

type wlConn struct {
	c      *net.UnixConn
	nextID uint32
	buf    []byte
	// Registry-Globals: Name -> (Interface, Version)
	globals []wlGlobal
	onEvent func(wlEvent) // Standard-Verteiler
}

type wlGlobal struct {
	name    uint32
	iface   string
	version uint32
}

func wlDial(path string) (*wlConn, error) {
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, err
	}
	c, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return nil, err
	}
	return &wlConn{c: c, nextID: 2}, nil
}

func (w *wlConn) Close() error { return w.c.Close() }

func (w *wlConn) newID() uint32 {
	id := w.nextID
	w.nextID++
	return id
}

// wlArgs baut die Argumente einer Nachricht.
type wlArgs struct {
	b   []byte
	fds []int
}

func (a *wlArgs) u32(v uint32) *wlArgs { a.b = binary.LittleEndian.AppendUint32(a.b, v); return a }
func (a *wlArgs) i32(v int32) *wlArgs  { return a.u32(uint32(v)) }
func (a *wlArgs) fixed(v float64) *wlArgs {
	return a.i32(int32(v * 256))
}
func (a *wlArgs) str(s string) *wlArgs {
	n := len(s) + 1
	a.u32(uint32(n))
	a.b = append(a.b, s...)
	a.b = append(a.b, 0)
	for len(a.b)%4 != 0 {
		a.b = append(a.b, 0)
	}
	return a
}
func (a *wlArgs) fd(fd int) *wlArgs { a.fds = append(a.fds, fd); return a }

func (w *wlConn) send(obj uint32, opcode uint16, a *wlArgs) error {
	if a == nil {
		a = &wlArgs{}
	}
	size := 8 + len(a.b)
	msg := make([]byte, 8, size)
	binary.LittleEndian.PutUint32(msg[0:], obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(size)<<16|uint32(opcode))
	msg = append(msg, a.b...)
	var oob []byte
	if len(a.fds) > 0 {
		oob = unix.UnixRights(a.fds...)
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _, err := w.c.WriteMsgUnix(msg, oob, nil)
	return err
}

// read liefert das nächste Event (blockierend bis deadline).
func (w *wlConn) read(deadline time.Time) (wlEvent, error) {
	for {
		if len(w.buf) >= 8 {
			obj := binary.LittleEndian.Uint32(w.buf[0:])
			so := binary.LittleEndian.Uint32(w.buf[4:])
			size := int(so >> 16)
			if size < 8 {
				return wlEvent{}, errors.New("wayland: ungültige Nachricht")
			}
			if len(w.buf) >= size {
				ev := wlEvent{obj: obj, opcode: uint16(so & 0xffff), data: append([]byte(nil), w.buf[8:size]...)}
				w.buf = w.buf[size:]
				return ev, nil
			}
		}
		_ = w.c.SetReadDeadline(deadline)
		tmp := make([]byte, 64<<10)
		oob := make([]byte, 256)
		n, oobn, _, _, err := w.c.ReadMsgUnix(tmp, oob)
		if oobn > 0 { // empfangene FDs (z. B. Keymap) schließen – brauchen wir nicht
			if msgs, perr := unix.ParseSocketControlMessage(oob[:oobn]); perr == nil {
				for _, m := range msgs {
					if fds, ferr := unix.ParseUnixRights(&m); ferr == nil {
						for _, fd := range fds {
							unix.Close(fd)
						}
					}
				}
			}
		}
		if n > 0 {
			w.buf = append(w.buf, tmp[:n]...)
		}
		if err != nil {
			if n > 0 {
				continue
			}
			return wlEvent{}, err
		}
		if n == 0 {
			return wlEvent{}, io.EOF
		}
	}
}

// Leser für Event-Argumente.
type wlReader struct {
	b []byte
}

func (r *wlReader) u32() uint32 {
	if len(r.b) < 4 {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}
func (r *wlReader) i32() int32 { return int32(r.u32()) }
func (r *wlReader) str() string {
	n := int(r.u32())
	if n == 0 || n > len(r.b) {
		return ""
	}
	s := string(r.b[:n-1])
	p := (n + 3) &^ 3
	if p > len(r.b) {
		p = len(r.b)
	}
	r.b = r.b[p:]
	return s
}

// roundtrip sendet wl_display.sync und verarbeitet Events bis zum Callback.
func (w *wlConn) roundtrip() error {
	cb := w.newID()
	if err := w.send(1, 0, (&wlArgs{}).u32(cb)); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ev, err := w.read(deadline)
		if err != nil {
			return err
		}
		if err := w.dispatchCore(ev); err != nil {
			return err
		}
		if ev.obj == cb && ev.opcode == 0 {
			return nil
		}
		if w.onEvent != nil {
			w.onEvent(ev)
		}
	}
}

// dispatchCore behandelt wl_display-Events (Fehler, delete_id).
func (w *wlConn) dispatchCore(ev wlEvent) error {
	if ev.obj == 1 && ev.opcode == 0 {
		r := &wlReader{b: ev.data}
		obj, code := r.u32(), r.u32()
		return fmt.Errorf("wayland-Fehler (Objekt %d, Code %d): %s", obj, code, r.str())
	}
	return nil
}

// registry holt die Globals.
func (w *wlConn) registry() (uint32, error) {
	reg := w.newID()
	if err := w.send(1, 1, (&wlArgs{}).u32(reg)); err != nil {
		return 0, err
	}
	prev := w.onEvent
	w.onEvent = func(ev wlEvent) {
		if ev.obj == reg && ev.opcode == 0 {
			r := &wlReader{b: ev.data}
			g := wlGlobal{name: r.u32()}
			g.iface = r.str()
			g.version = r.u32()
			w.globals = append(w.globals, g)
		} else if prev != nil {
			prev(ev)
		}
	}
	err := w.roundtrip()
	w.onEvent = prev
	return reg, err
}

// bind bindet ein Global (Version = min(Server, max)) und liefert die neue Objekt-ID.
func (w *wlConn) bind(reg uint32, g wlGlobal, max uint32) (uint32, uint32, error) {
	v := g.version
	if v > max {
		v = max
	}
	id := w.newID()
	err := w.send(reg, 0, (&wlArgs{}).u32(g.name).str(g.iface).u32(v).u32(id))
	return id, v, err
}

func (w *wlConn) find(iface string) []wlGlobal {
	var out []wlGlobal
	for _, g := range w.globals {
		if g.iface == iface {
			out = append(out, g)
		}
	}
	return out
}
