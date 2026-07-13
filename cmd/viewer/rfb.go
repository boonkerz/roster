package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"strings"
	"sync"
)

// Minimaler RFB/VNC-3.8-Client passend zum eingebauten Server (internal/agent/
// remote/rfb.go): Security "None", festes Pixelformat BGRX (32bpp), Encodings Raw
// (0) und Tight-JPEG (7, nur Kontrollbyte 0x90). Kein Fremd-Code.

// rectUpdate ist ein dekodiertes Rechteck im Server-Pixelformat BGRX. Bei resize=true
// ist es kein Pixel-Update, sondern eine neue Framebuffer-Größe (DesktopSize).
type rectUpdate struct {
	x, y, w, h int
	pix        []byte // len = w*h*4
	resize     bool
}

type rfbClient struct {
	br   *bufio.Reader
	w    io.Writer
	wmu  sync.Mutex // serialisiert Schreibzugriffe (Input + Frame-Requests)
	W, H int
}

// rfbHandshake führt den Client-Handshake bis ServerInit durch und liefert die
// Framebuffer-Größe.
func rfbHandshake(conn io.ReadWriter) (*rfbClient, error) {
	br := bufio.NewReaderSize(conn, 1<<16)

	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		return nil, fmt.Errorf("protocolversion: %w", err)
	}
	if !strings.HasPrefix(string(ver), "RFB ") {
		return nil, fmt.Errorf("kein RFB-Server (Antwort %q)", string(ver))
	}
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return nil, err
	}

	// Security: Liste der Typen; wir wählen "None" (1).
	nb := make([]byte, 1)
	if _, err := io.ReadFull(br, nb); err != nil {
		return nil, err
	}
	if nb[0] == 0 {
		return nil, fmt.Errorf("server lehnte die verbindung ab")
	}
	types := make([]byte, int(nb[0]))
	if _, err := io.ReadFull(br, types); err != nil {
		return nil, err
	}
	none := false
	for _, t := range types {
		if t == 1 {
			none = true
		}
	}
	if !none {
		return nil, fmt.Errorf("server verlangt VNC-Authentifizierung (nicht unterstützt)")
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		return nil, err
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(br, res); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint32(res) != 0 {
		return nil, fmt.Errorf("security-handshake fehlgeschlagen")
	}

	// ClientInit (shared) → ServerInit.
	if _, err := conn.Write([]byte{1}); err != nil {
		return nil, err
	}
	hdr := make([]byte, 24) // 2 w + 2 h + 16 pixelformat + 4 name-len
	if _, err := io.ReadFull(br, hdr); err != nil {
		return nil, err
	}
	c := &rfbClient{
		br: br, w: conn,
		W: int(binary.BigEndian.Uint16(hdr[0:])),
		H: int(binary.BigEndian.Uint16(hdr[2:])),
	}
	if n := int(binary.BigEndian.Uint32(hdr[20:])); n > 0 {
		if _, err := io.CopyN(io.Discard, br, int64(n)); err != nil {
			return nil, err
		}
	}
	if c.W == 0 || c.H == 0 {
		return nil, fmt.Errorf("ungültige framebuffer-größe %dx%d", c.W, c.H)
	}
	return c, nil
}

func (c *rfbClient) write(b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.w.Write(b)
	return err
}

// setEncodings meldet die unterstützten Encodings (Tight bevorzugt, Raw als Fallback).
func (c *rfbClient) setEncodings(encs ...int32) error {
	b := []byte{2, 0, byte(len(encs) >> 8), byte(len(encs))}
	for _, e := range encs {
		b = append(b, byte(e>>24), byte(e>>16), byte(e>>8), byte(e))
	}
	return c.write(b)
}

func (c *rfbClient) requestUpdate(incremental bool) error {
	inc := byte(0)
	if incremental {
		inc = 1
	}
	return c.write([]byte{3, inc, 0, 0, 0, 0, byte(c.W >> 8), byte(c.W), byte(c.H >> 8), byte(c.H)})
}

func (c *rfbClient) keyEvent(down bool, keysym uint32) error {
	d := byte(0)
	if down {
		d = 1
	}
	return c.write([]byte{4, d, 0, 0, byte(keysym >> 24), byte(keysym >> 16), byte(keysym >> 8), byte(keysym)})
}

func (c *rfbClient) pointerEvent(mask, x, y int) error {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return c.write([]byte{5, byte(mask), byte(x >> 8), byte(x), byte(y >> 8), byte(y)})
}

// readLoop dekodiert Server-Nachrichten und schiebt Rechteck-Updates in updates.
// Nach jedem FramebufferUpdate wird sofort ein inkrementeller Nachschub angefordert.
func (c *rfbClient) readLoop(updates chan<- rectUpdate, cut chan<- string) error {
	hdr := make([]byte, 1)
	for {
		if _, err := io.ReadFull(c.br, hdr); err != nil {
			return err
		}
		switch hdr[0] {
		case 0: // FramebufferUpdate
			b := make([]byte, 3)
			if _, err := io.ReadFull(c.br, b); err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint16(b[1:]))
			for i := 0; i < n; i++ {
				if err := c.readRect(updates); err != nil {
					return err
				}
			}
			if err := c.requestUpdate(true); err != nil {
				return err
			}
		case 2: // Bell
		case 3: // ServerCutText (Gerät → Viewer)
			b := make([]byte, 7)
			if _, err := io.ReadFull(c.br, b); err != nil {
				return err
			}
			txt := make([]byte, int(binary.BigEndian.Uint32(b[3:])))
			if _, err := io.ReadFull(c.br, txt); err != nil {
				return err
			}
			select {
			case cut <- latin1Decode(txt):
			default:
			}
		default:
			return fmt.Errorf("unbekannte server-nachricht %d", hdr[0])
		}
	}
}

func (c *rfbClient) readRect(updates chan<- rectUpdate) error {
	rh := make([]byte, 12)
	if _, err := io.ReadFull(c.br, rh); err != nil {
		return err
	}
	x := int(binary.BigEndian.Uint16(rh[0:]))
	y := int(binary.BigEndian.Uint16(rh[2:]))
	w := int(binary.BigEndian.Uint16(rh[4:]))
	h := int(binary.BigEndian.Uint16(rh[6:]))
	enc := int32(binary.BigEndian.Uint32(rh[8:]))
	if w == 0 || h == 0 {
		return nil
	}
	switch enc {
	case -223: // DesktopSize: neue Framebuffer-Größe (Auflösungswechsel am Gerät)
		c.W, c.H = w, h
		updates <- rectUpdate{w: w, h: h, resize: true}
	case 0: // Raw (BGRX)
		pix := make([]byte, w*h*4)
		if _, err := io.ReadFull(c.br, pix); err != nil {
			return err
		}
		updates <- rectUpdate{x: x, y: y, w: w, h: h, pix: pix}
	case 7: // Tight – der Server sendet ausschließlich JPEG (Kontrollbyte 0x9x).
		ctrl := make([]byte, 1)
		if _, err := io.ReadFull(c.br, ctrl); err != nil {
			return err
		}
		if ctrl[0]>>4 != 0x9 {
			return fmt.Errorf("nicht unterstützte Tight-Kodierung 0x%02x", ctrl[0])
		}
		n, err := readCompactLen(c.br)
		if err != nil {
			return err
		}
		jb := make([]byte, n)
		if _, err := io.ReadFull(c.br, jb); err != nil {
			return err
		}
		img, err := jpeg.Decode(bytes.NewReader(jb))
		if err != nil {
			return fmt.Errorf("jpeg: %w", err)
		}
		updates <- rectUpdate{x: x, y: y, w: w, h: h, pix: jpegToBGRX(img, w, h)}
	default:
		return fmt.Errorf("nicht unterstützte Kodierung %d", enc)
	}
	return nil
}

// jpegToBGRX konvertiert ein dekodiertes JPEG in das Server-Pixelformat BGRX.
func jpegToBGRX(img image.Image, w, h int) []byte {
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba.Rect.Dx() != w || rgba.Rect.Dy() != h || rgba.Stride != w*4 {
		tmp := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(tmp, tmp.Bounds(), img, img.Bounds().Min, draw.Src)
		rgba = tmp
	}
	pix := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		pix[i*4+0] = rgba.Pix[i*4+2] // B
		pix[i*4+1] = rgba.Pix[i*4+1] // G
		pix[i*4+2] = rgba.Pix[i*4+0] // R
		pix[i*4+3] = 255
	}
	return pix
}

// readCompactLen liest eine Tight-kompakte Länge (Spiegel von compactLen im Server).
func readCompactLen(r io.Reader) (int, error) {
	b := make([]byte, 1)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	n := int(b[0] & 0x7f)
	if b[0]&0x80 != 0 {
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		n |= int(b[0]&0x7f) << 7
		if b[0]&0x80 != 0 {
			if _, err := io.ReadFull(r, b); err != nil {
				return 0, err
			}
			n |= int(b[0]) << 14
		}
	}
	return n, nil
}

func latin1Decode(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}
