//go:build linux

package remote

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// waylandSource nimmt den Bildschirm eines wlroots-Compositors (labwc, Wayfire, Sway …)
// über wlr-screencopy auf und spielt Maus/Tastatur über wlr-virtual-pointer und
// zwp-virtual-keyboard ein.

var errNoScreencopy = errors.New("Compositor bietet kein wlr-screencopy (GNOME/KDE unter Wayland werden noch nicht unterstützt)")

const (
	wlShmARGB8888 = 0
	wlShmXRGB8888 = 1
	wlShmABGR8888 = 0x34324241
	wlShmXBGR8888 = 0x34324258
)

type waylandSource struct {
	c         *wlConn
	output    uint32
	shm       uint32
	sc        uint32
	scVersion uint32
	vptr      uint32
	vkbd      uint32
	km        *wlKeymap
	start     time.Time
	prevMask  int

	pool, buffer          uint32
	memfd                 int
	mem                   []byte
	bufW, bufH, bufStride int
	bufFormat             uint32
	w, h                  int
	out                   []byte
}

func newWaylandSource(sock string, monitor int) (*waylandSource, error) {
	c, err := wlDial(sock)
	if err != nil {
		return nil, err
	}
	s := &waylandSource{c: c, memfd: -1, start: time.Now()}
	reg, err := c.registry()
	if err != nil {
		c.Close()
		return nil, err
	}
	scG := c.find("zwlr_screencopy_manager_v1")
	shmG := c.find("wl_shm")
	outs := c.find("wl_output")
	if len(scG) == 0 {
		c.Close()
		return nil, errNoScreencopy
	}
	if len(shmG) == 0 || len(outs) == 0 {
		c.Close()
		return nil, errors.New("wayland: wl_shm oder wl_output fehlt")
	}
	if monitor < 0 || monitor >= len(outs) {
		monitor = 0
	}
	if s.sc, s.scVersion, err = c.bind(reg, scG[0], 3); err != nil {
		return nil, s.fail(err)
	}
	if s.shm, _, err = c.bind(reg, shmG[0], 1); err != nil {
		return nil, s.fail(err)
	}
	if s.output, _, err = c.bind(reg, outs[monitor], 3); err != nil {
		return nil, s.fail(err)
	}
	// Eingaben (optional – ohne die Protokolle bleibt die Sitzung view-only).
	if seats := c.find("wl_seat"); len(seats) > 0 {
		seat, _, err := c.bind(reg, seats[0], 5)
		if err == nil {
			if vp := c.find("zwlr_virtual_pointer_manager_v1"); len(vp) > 0 {
				if m, v, err := c.bind(reg, vp[0], 2); err == nil {
					s.vptr = c.newID()
					if v >= 2 {
						_ = c.send(m, 2, (&wlArgs{}).u32(seat).u32(s.output).u32(s.vptr))
					} else {
						_ = c.send(m, 0, (&wlArgs{}).u32(seat).u32(s.vptr))
					}
				}
			}
			if vk := c.find("zwp_virtual_keyboard_manager_v1"); len(vk) > 0 {
				if m, _, err := c.bind(reg, vk[0], 1); err == nil {
					s.vkbd = c.newID()
					_ = c.send(m, 0, (&wlArgs{}).u32(seat).u32(s.vkbd))
					s.km = buildWlKeymap()
					if err := s.sendKeymap(); err != nil {
						s.vkbd = 0
					}
				}
			}
		}
	}
	if err := c.roundtrip(); err != nil {
		return nil, s.fail(err)
	}
	// Erstes Bild bestimmt die Größe.
	if _, err := s.grab(); err != nil {
		return nil, s.fail(err)
	}
	s.w, s.h = s.bufW, s.bufH
	return s, nil
}

func (s *waylandSource) fail(err error) error {
	s.Close()
	return err
}

func (s *waylandSource) sendKeymap() error {
	data := append([]byte(s.km.text), 0)
	fd, err := unix.MemfdCreate("roster-keymap", unix.MFD_CLOEXEC)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if _, err := unix.Write(fd, data); err != nil {
		return err
	}
	return s.c.send(s.vkbd, 0, (&wlArgs{}).u32(1).fd(fd).u32(uint32(len(data))))
}

func (s *waylandSource) Bounds() (int, int) { return s.w, s.h }

func (s *waylandSource) Capture() ([]byte, error) {
	px, err := s.grab()
	if err != nil {
		return nil, err
	}
	if s.bufW != s.w || s.bufH != s.h { // Auflösung geändert -> Neuaufbau
		return nil, fmt.Errorf("wayland: Größe geändert (%dx%d)", s.bufW, s.bufH)
	}
	return px, nil
}

// grab nimmt ein Bild auf (screencopy-Ablauf: Puffer-Angebot → copy → ready).
func (s *waylandSource) grab() ([]byte, error) {
	frame := s.c.newID()
	if err := s.c.send(s.sc, 0, (&wlArgs{}).u32(frame).i32(1).u32(s.output)); err != nil {
		return nil, err
	}
	defer s.c.send(frame, 1, nil) //nolint:errcheck
	var format uint32
	var fw, fh, stride int
	gotBuffer, copied, yInvert := false, false, false
	deadline := time.Now().Add(5 * time.Second)
	for {
		ev, err := s.c.read(deadline)
		if err != nil {
			return nil, err
		}
		if err := s.c.dispatchCore(ev); err != nil {
			return nil, err
		}
		if ev.obj != frame {
			continue
		}
		r := &wlReader{b: ev.data}
		switch ev.opcode {
		case 0: // buffer(format, width, height, stride)
			f, w, h, st := r.u32(), int(r.u32()), int(r.u32()), int(r.u32())
			if !gotBuffer && supportedShm(f) {
				format, fw, fh, stride, gotBuffer = f, w, h, st, true
			}
			if s.scVersion < 3 && gotBuffer && !copied {
				if err := s.copyInto(frame, format, fw, fh, stride); err != nil {
					return nil, err
				}
				copied = true
			}
		case 1: // flags
			yInvert = r.u32()&1 != 0
		case 6: // buffer_done (v3)
			if !gotBuffer {
				return nil, errors.New("wayland: kein unterstütztes Pixelformat angeboten")
			}
			if !copied {
				if err := s.copyInto(frame, format, fw, fh, stride); err != nil {
					return nil, err
				}
				copied = true
			}
		case 2: // ready
			return s.convert(yInvert), nil
		case 3: // failed
			return nil, errors.New("wayland: Aufnahme fehlgeschlagen")
		}
	}
}

func supportedShm(f uint32) bool {
	return f == wlShmARGB8888 || f == wlShmXRGB8888 || f == wlShmABGR8888 || f == wlShmXBGR8888
}

func (s *waylandSource) copyInto(frame, format uint32, w, h, stride int) error {
	if s.buffer == 0 || w != s.bufW || h != s.bufH || stride != s.bufStride || format != s.bufFormat {
		s.freeBuffer()
		size := stride * h
		fd, err := unix.MemfdCreate("roster-screencopy", unix.MFD_CLOEXEC)
		if err != nil {
			return err
		}
		if err := unix.Ftruncate(fd, int64(size)); err != nil {
			unix.Close(fd)
			return err
		}
		mem, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			unix.Close(fd)
			return err
		}
		s.memfd, s.mem = fd, mem
		s.pool = s.c.newID()
		if err := s.c.send(s.shm, 0, (&wlArgs{}).u32(s.pool).fd(fd).i32(int32(size))); err != nil {
			return err
		}
		s.buffer = s.c.newID()
		if err := s.c.send(s.pool, 0, (&wlArgs{}).u32(s.buffer).i32(0).i32(int32(w)).i32(int32(h)).i32(int32(stride)).u32(format)); err != nil {
			return err
		}
		s.bufW, s.bufH, s.bufStride, s.bufFormat = w, h, stride, format
		s.out = make([]byte, w*h*4)
	}
	return s.c.send(frame, 0, (&wlArgs{}).u32(s.buffer))
}

// convert schreibt den Puffer als BGRX (Zeile für Zeile, ggf. gespiegelt/getauscht).
func (s *waylandSource) convert(yInvert bool) []byte {
	return convertShm(s.mem, s.out, s.bufW, s.bufH, s.bufStride, s.bufFormat, yInvert)
}

// convertShm wandelt einen wl_shm-Puffer nach BGRX (rein, getestet).
func convertShm(src, dst []byte, w, h, stride int, format uint32, yInvert bool) []byte {
	swap := format == wlShmABGR8888 || format == wlShmXBGR8888
	for y := 0; y < h; y++ {
		sy := y
		if yInvert {
			sy = h - 1 - y
		}
		srow := src[sy*stride : sy*stride+w*4]
		drow := dst[y*w*4 : (y+1)*w*4]
		if !swap {
			copy(drow, srow)
			continue
		}
		for x := 0; x < w*4; x += 4 {
			drow[x], drow[x+1], drow[x+2], drow[x+3] = srow[x+2], srow[x+1], srow[x], 0
		}
	}
	return dst
}

func (s *waylandSource) freeBuffer() {
	if s.buffer != 0 {
		_ = s.c.send(s.buffer, 0, nil)
		s.buffer = 0
	}
	if s.pool != 0 {
		_ = s.c.send(s.pool, 1, nil)
		s.pool = 0
	}
	if s.mem != nil {
		_ = unix.Munmap(s.mem)
		s.mem = nil
	}
	if s.memfd >= 0 {
		unix.Close(s.memfd)
		s.memfd = -1
	}
}

func (s *waylandSource) ms() uint32 { return uint32(time.Since(s.start).Milliseconds()) }

// Linux-Eingabe-Codes (input-event-codes.h).
const (
	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
)

func (s *waylandSource) Pointer(mask, x, y int) {
	if s.vptr == 0 || s.w == 0 || s.h == 0 {
		return
	}
	t := s.ms()
	_ = s.c.send(s.vptr, 1, (&wlArgs{}).u32(t).u32(uint32(clampInt(x, 0, s.w-1))).u32(uint32(clampInt(y, 0, s.h-1))).u32(uint32(s.w)).u32(uint32(s.h)))
	for bit, btn := range map[int]uint32{0: btnLeft, 1: btnMiddle, 2: btnRight} {
		was, is := s.prevMask&(1<<bit) != 0, mask&(1<<bit) != 0
		if was != is {
			state := uint32(0)
			if is {
				state = 1
			}
			_ = s.c.send(s.vptr, 2, (&wlArgs{}).u32(t).u32(btn).u32(state))
		}
	}
	// Rad: Bits 3/4 = hoch/runter, 5/6 = links/rechts (je Druck ein Rastschritt).
	for bit, ax := range map[int][2]int{3: {0, -1}, 4: {0, 1}, 5: {1, -1}, 6: {1, 1}} {
		if mask&(1<<bit) != 0 && s.prevMask&(1<<bit) == 0 {
			_ = s.c.send(s.vptr, 5, (&wlArgs{}).u32(0))                                                               // axis_source wheel
			_ = s.c.send(s.vptr, 7, (&wlArgs{}).u32(t).u32(uint32(ax[0])).fixed(float64(15*ax[1])).i32(int32(ax[1]))) // axis_discrete
		}
	}
	_ = s.c.send(s.vptr, 4, nil) // frame
	s.prevMask = mask
}

func (s *waylandSource) Key(down bool, keysym uint32) {
	if s.vkbd == 0 || s.km == nil {
		return
	}
	code, ok := s.km.keycode(keysym)
	if !ok {
		return
	}
	state := uint32(0)
	if down {
		state = 1
	}
	_ = s.c.send(s.vkbd, 1, (&wlArgs{}).u32(s.ms()).u32(code).u32(state))
}

func (s *waylandSource) Close() error {
	if s.c == nil {
		return nil
	}
	s.freeBuffer()
	if s.vkbd != 0 {
		_ = s.c.send(s.vkbd, 3, nil)
	}
	if s.vptr != 0 {
		_ = s.c.send(s.vptr, 8, nil)
	}
	err := s.c.Close()
	s.c = nil
	return err
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
