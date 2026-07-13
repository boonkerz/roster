package remote

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"time"

	"log/slog"
)

// Eigener, minimaler RFB/VNC-Server (view-only): überträgt den Bildschirm als
// Raw-Encoding an einen RFB-Client (noVNC). Keine Fremd-Software, keine Bundles –
// der „VNC-Server" ist der Agent selbst. Eingaben (Maus/Tastatur) folgen später.
//
// Ablauf (RFB 3.8, Security = None): ProtocolVersion → Security → ClientInit →
// ServerInit → Schleife aus FramebufferUpdateRequest → FramebufferUpdate.

// screenSource liefert den aktuellen Bildschirminhalt als 32-bpp-Pixel in der
// Reihenfolge B,G,R,X (little-endian 0x00RRGGBB) – passend zum unten deklarierten
// Pixelformat.
type screenSource interface {
	Bounds() (w, h int)
	Capture() ([]byte, error) // Länge = w*h*4
	Close() error
}

// inputSink nimmt Maus-/Tastatureingaben entgegen. Quellen, die das umsetzen können
// (Windows), spielen sie ins System ein; sonst bleibt die Sitzung view-only.
// x,y sind Framebuffer-Koordinaten; keysym ist ein X11-Keysym (RFB).
type inputSink interface {
	Pointer(buttonMask, x, y int)
	Key(down bool, keysym uint32)
}

// clipboardSource synchronisiert die Zwischenablage. GetClipboard liefert den Text
// nur, wenn er sich seit dem letzten Aufruf geändert hat.
type clipboardSource interface {
	GetClipboard() (text string, changed bool)
	SetClipboard(text string)
}

// latin1Decode/Encode: RFB-CutText ist Latin-1 (Zeichen > 255 gehen verloren).
func latin1Decode(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func latin1Encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 255 {
			r = '?'
		}
		out = append(out, byte(r))
	}
	return out
}

// rfbPixelFormat: 32 bpp, Tiefe 24, little-endian, TrueColor, Shifts R=16/G=8/B=0.
func rfbPixelFormat() []byte {
	return []byte{
		32, 24, 0, 1, // bpp, depth, big-endian=0, true-colour=1
		0, 255, 0, 255, 0, 255, // red/green/blue max (je 2 Byte BE)
		16, 8, 0, // red/green/blue shift
		0, 0, 0, // padding
	}
}

func rfbServe(ctx context.Context, conn io.ReadWriter, src screenSource, log *slog.Logger) error {
	w, h := src.Bounds()

	// 1. ProtocolVersion
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	if err := skip(conn, 12); err != nil {
		return err
	}
	// 2. Security: nur „None" (1). Tunnel + RBAC sind die Sicherheitsgrenze.
	if _, err := conn.Write([]byte{1, 1}); err != nil {
		return err
	}
	if err := skip(conn, 1); err != nil { // gewählter Security-Typ
		return err
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil { // SecurityResult OK
		return err
	}
	// 3. ClientInit (shared-flag) ignorieren
	if err := skip(conn, 1); err != nil {
		return err
	}
	// 4. ServerInit
	si := make([]byte, 0, 32)
	si = be16(si, w)
	si = be16(si, h)
	si = append(si, rfbPixelFormat()...)
	name := []byte("PC-Inventory")
	si = be32(si, len(name))
	si = append(si, name...)
	if _, err := conn.Write(si); err != nil {
		return err
	}

	var lastFrame, lastClipPoll time.Time
	tight := false // Client unterstützt Tight-Encoding (7)?
	fctx := &frameCtx{}
	hdr := make([]byte, 1)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return err
		}
		switch hdr[0] {
		case 0: // SetPixelFormat (3 padding + 16 pf) – wir bleiben bei unserem Format
			if err := skip(conn, 19); err != nil {
				return err
			}
		case 2: // SetEncodings (1 padding + 2 count + count*4)
			b := make([]byte, 3)
			if _, err := io.ReadFull(conn, b); err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint16(b[1:]))
			encs := make([]byte, n*4)
			if _, err := io.ReadFull(conn, encs); err != nil {
				return err
			}
			for i := 0; i < n; i++ {
				if int32(binary.BigEndian.Uint32(encs[i*4:])) == 7 { // Tight
					tight = true
				}
			}
		case 3: // FramebufferUpdateRequest: incremental(1) + x,y,w,h (je 2)
			req := make([]byte, 9)
			if _, err := io.ReadFull(conn, req); err != nil {
				return err
			}
			// Ratenbegrenzung (~30 fps), damit wir nicht durchdrehen
			if d := time.Since(lastFrame); d < 33*time.Millisecond {
				time.Sleep(33*time.Millisecond - d)
			}
			lastFrame = time.Now()
			incremental := req[0] != 0
			// Auflösungswechsel? Neue Größe an den Client melden (DesktopSize) und ein
			// frisches Vollbild in der neuen Größe schicken.
			if nw, nh := src.Bounds(); (nw != w || nh != h) && nw > 0 && nh > 0 {
				w, h = nw, nh
				fctx = &frameCtx{}
				incremental = false
				if err := writeDesktopSize(conn, w, h); err != nil {
					return err
				}
			}
			if err := fctx.send(conn, src, w, h, incremental, tight); err != nil {
				return err
			}
			// Zwischenablage Gerät → Browser (gedrosselt pollen).
			if cs, ok := src.(clipboardSource); ok && time.Since(lastClipPoll) > 700*time.Millisecond {
				lastClipPoll = time.Now()
				if text, changed := cs.GetClipboard(); changed {
					msg := []byte{3, 0, 0, 0}
					b := latin1Encode(text)
					msg = be32(msg, len(b))
					if _, err := conn.Write(append(msg, b...)); err != nil {
						return err
					}
				}
			}
		case 4: // KeyEvent: down-flag(1) + padding(2) + keysym(4)
			b := make([]byte, 7)
			if _, err := io.ReadFull(conn, b); err != nil {
				return err
			}
			if in, ok := src.(inputSink); ok {
				in.Key(b[0] != 0, binary.BigEndian.Uint32(b[3:]))
			}
		case 5: // PointerEvent: button-mask(1) + x(2) + y(2)
			b := make([]byte, 5)
			if _, err := io.ReadFull(conn, b); err != nil {
				return err
			}
			if in, ok := src.(inputSink); ok {
				in.Pointer(int(b[0]), int(binary.BigEndian.Uint16(b[1:])), int(binary.BigEndian.Uint16(b[3:])))
			}
		case 6: // ClientCutText: 3 padding + 4 length + Text (Browser → Gerät)
			b := make([]byte, 7)
			if _, err := io.ReadFull(conn, b); err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint32(b[3:]))
			text := make([]byte, n)
			if _, err := io.ReadFull(conn, text); err != nil {
				return err
			}
			if cs, ok := src.(clipboardSource); ok && n > 0 {
				cs.SetClipboard(latin1Decode(text))
			}
		default:
			return fmt.Errorf("unbekannte RFB-Nachricht %d", hdr[0])
		}
	}
}

// jpegMinArea: ab dieser Fläche (Pixel) wird ein geänderter Bereich als Tight-JPEG
// gesendet (klein = Text/Cursor → Raw/verlustfrei; groß = Bewegung/Foto → JPEG).
const jpegMinArea = 96 * 96

// frameCtx hält den Zustand über Frames hinweg: letztes Vollbild (für Diff),
// zuletzt verlustbehaftet (JPEG) gesendeter Bereich und Zeitpunkt der letzten
// Änderung – für adaptive Qualität und den Nachschärf-Refresh im Leerlauf.
type frameCtx struct {
	prev               []byte
	hasLossy           bool
	lx0, ly0, lx1, ly1 int
	lastChange         time.Time
}

// send überträgt einen Update: geänderten Bereich (Dirty-Rect) als Raw (klein) oder
// Tight-JPEG (groß, adaptive Qualität). Ohne Änderung ggf. verlustfreier Refresh
// des zuletzt als JPEG gesendeten Bereichs (Text wird nach Bewegung wieder scharf).
func (st *frameCtx) send(conn io.Writer, src screenSource, w, h int, incremental, tight bool) error {
	cur, err := src.Capture()
	if err != nil || len(cur) != w*h*4 {
		// Fehler oder Größen-Mismatch (Auflösung gerade gewechselt) -> leeres Update;
		// der Größenwechsel wird im nächsten Request über DesktopSize aufgelöst.
		_, werr := conn.Write([]byte{0, 0, 0, 0})
		return werr
	}

	x0, y0, x1, y1 := 0, 0, w-1, h-1
	if incremental && st.prev != nil {
		x0, y0, x1, y1 = diffRect(st.prev, cur, w, h)
		if x1 < x0 { // keine Änderung
			// Leerlauf: den zuletzt lossy (JPEG) gesendeten Bereich verlustfrei
			// nachschärfen, wenn seit der letzten Änderung genug Zeit verging.
			if st.hasLossy && time.Since(st.lastChange) > 250*time.Millisecond {
				st.hasLossy = false
				st.writeRect(conn, cur, st.lx0, st.ly0, st.lx1, st.ly1, w, false, 0)
				st.store(cur)
				return nil
			}
			_, werr := conn.Write([]byte{0, 0, 0, 0})
			return werr
		}
	}
	st.lastChange = time.Now()
	rw, rh := x1-x0+1, y1-y0+1

	useJPEG := tight && rw*rh >= jpegMinArea
	quality := 85
	if useJPEG { // bei viel Bewegung Qualität senken -> flüssiger
		frac := rw * rh
		switch {
		case frac > w*h/2:
			quality = 45
		case frac > w*h/6:
			quality = 65
		}
	}
	if err := st.writeRect(conn, cur, x0, y0, x1, y1, w, useJPEG, quality); err != nil {
		return err
	}
	st.hasLossy = useJPEG
	if useJPEG {
		st.lx0, st.ly0, st.lx1, st.ly1 = x0, y0, x1, y1
	}
	st.store(cur)
	return nil
}

func (st *frameCtx) store(cur []byte) {
	if len(st.prev) != len(cur) {
		st.prev = make([]byte, len(cur))
	}
	copy(st.prev, cur)
}

// writeRect schreibt ein FramebufferUpdate mit einem Rechteck (Raw oder Tight-JPEG).
func (st *frameCtx) writeRect(conn io.Writer, cur []byte, x0, y0, x1, y1, w int, useJPEG bool, quality int) error {
	rw, rh := x1-x0+1, y1-y0+1
	enc := 0
	if useJPEG {
		enc = 7
	}
	hdr := make([]byte, 0, 16)
	hdr = append(hdr, 0, 0)
	hdr = be16(hdr, 1)
	hdr = be16(hdr, x0)
	hdr = be16(hdr, y0)
	hdr = be16(hdr, rw)
	hdr = be16(hdr, rh)
	hdr = be32(hdr, enc)
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	if useJPEG {
		return writeTightJPEG(conn, cur, x0, y0, rw, rh, w, quality)
	}
	return writeRawRect(conn, cur, x0, y0, x1, y1, w)
}

// writeRawRect schreibt die Pixel eines Teilrechtecks (BGRX) zeilenweise; Chunks
// unter dem WS-Nachrichtenlimit.
func writeRawRect(conn io.Writer, cur []byte, x0, y0, x1, y1, w int) error {
	stride := w * 4
	buf := make([]byte, 0, 256*1024)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		_, err := conn.Write(buf)
		buf = buf[:0]
		return err
	}
	for y := y0; y <= y1; y++ {
		row := cur[y*stride+x0*4 : y*stride+(x1+1)*4]
		if len(buf)+len(row) > 256*1024 {
			if err := flush(); err != nil {
				return err
			}
		}
		buf = append(buf, row...)
	}
	return flush()
}

// writeTightJPEG kodiert den Bereich als JPEG und sendet ihn als Tight-JPEG-
// Rechteck (Control 0x90 + kompakte Länge + JPEG-Daten).
func writeTightJPEG(conn io.Writer, cur []byte, x0, y0, rw, rh, w, quality int) error {
	img := image.NewRGBA(image.Rect(0, 0, rw, rh))
	stride := w * 4
	for y := 0; y < rh; y++ {
		src := (y0+y)*stride + x0*4
		dst := y * img.Stride
		for x := 0; x < rw; x++ {
			s := src + x*4
			d := dst + x*4
			img.Pix[d+0] = cur[s+2] // R (Quelle ist BGRX)
			img.Pix[d+1] = cur[s+1] // G
			img.Pix[d+2] = cur[s+0] // B
			img.Pix[d+3] = 255
		}
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, img, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	ctrl := []byte{0x90} // Tight: JpegCompression
	if _, err := conn.Write(append(ctrl, compactLen(jb.Len())...)); err != nil {
		return err
	}
	_, err := conn.Write(jb.Bytes())
	return err
}

// compactLen kodiert eine Länge im Tight-Format (7 Bit je Byte, MSB = Fortsetzung).
func compactLen(n int) []byte {
	b := []byte{byte(n & 0x7f)}
	if n > 0x7f {
		b[0] |= 0x80
		b = append(b, byte((n>>7)&0x7f))
		if n > 0x3fff {
			b[1] |= 0x80
			b = append(b, byte((n>>14)&0xff))
		}
	}
	return b
}

// diffRect liefert die Bounding-Box (x0,y0)-(x1,y1) der geänderten Pixel zwischen
// prev und cur. Bei keiner Änderung ist x1 < x0.
func diffRect(prev, cur []byte, w, h int) (int, int, int, int) {
	stride := w * 4
	x0, y0, x1, y1 := w, h, -1, -1
	for y := 0; y < h; y++ {
		rs := y * stride
		if bytes.Equal(prev[rs:rs+stride], cur[rs:rs+stride]) {
			continue
		}
		if y < y0 {
			y0 = y
		}
		y1 = y
		// linke Kante
		lx := 0
		for lx < w && prev[rs+lx*4] == cur[rs+lx*4] && prev[rs+lx*4+1] == cur[rs+lx*4+1] &&
			prev[rs+lx*4+2] == cur[rs+lx*4+2] {
			lx++
		}
		if lx < x0 {
			x0 = lx
		}
		// rechte Kante
		rx := w - 1
		for rx > lx && prev[rs+rx*4] == cur[rs+rx*4] && prev[rs+rx*4+1] == cur[rs+rx*4+1] &&
			prev[rs+rx*4+2] == cur[rs+rx*4+2] {
			rx--
		}
		if rx > x1 {
			x1 = rx
		}
	}
	return x0, y0, x1, y1
}

// writeDesktopSize meldet dem Client eine neue Framebuffer-Größe über das
// DesktopSize-Pseudo-Encoding (-223): ein FramebufferUpdate mit einem Rechteck
// (0,0,w,h) und Encoding -223, ohne Pixeldaten. noVNC und der native Viewer
// passen daraufhin ihre Framebuffer-/Fenstergröße an.
func writeDesktopSize(conn io.Writer, w, h int) error {
	msg := []byte{0, 0} // FramebufferUpdate + Padding
	msg = be16(msg, 1)  // 1 Rechteck
	msg = be16(msg, 0)  // x
	msg = be16(msg, 0)  // y
	msg = be16(msg, w)
	msg = be16(msg, h)
	msg = be32(msg, -223) // Encoding DesktopSize
	_, err := conn.Write(msg)
	return err
}

func be16(b []byte, v int) []byte { return append(b, byte(v>>8), byte(v)) }
func be32(b []byte, v int) []byte { return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)) }

// skip liest n Bytes und verwirft sie.
func skip(r io.Reader, n int) error {
	if n <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}
