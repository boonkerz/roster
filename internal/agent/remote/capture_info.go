package remote

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// infoSource zeigt statt eines Bildschirms einen Hinweis (keine Sitzung, nicht
// unterstütztes System). Nach einigen Sekunden meldet Capture einen Fehler, damit
// resilientSource neu sucht – meldet sich jemand an, erscheint der Desktop von selbst.
type infoSource struct {
	buf     []byte
	w, h    int
	created time.Time
}

func newInfoSource(lines ...string) *infoSource {
	const w, h, scale = 1280, 720, 2
	small := image.NewRGBA(image.Rect(0, 0, w/scale, h/scale))
	draw.Draw(small, small.Bounds(), &image.Uniform{color.RGBA{0x1b, 0x22, 0x30, 0xff}}, image.Point{}, draw.Src)
	y := (h/scale)/2 - len(lines)*9
	for i, l := range lines {
		col := color.RGBA{0xc8, 0xcd, 0xd8, 0xff}
		if i == 0 {
			col = color.RGBA{0xff, 0xff, 0xff, 0xff}
		}
		d := &font.Drawer{Dst: small, Src: &image.Uniform{col}, Face: basicfont.Face7x13}
		width := d.MeasureString(l).Ceil()
		d.Dot = fixed.P((w/scale-width)/2, y+i*18)
		d.DrawString(l)
	}
	buf := make([]byte, w*h*4)
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			c := small.RGBAAt(px/scale, py/scale)
			i := (py*w + px) * 4
			buf[i], buf[i+1], buf[i+2] = c.B, c.G, c.R
		}
	}
	return &infoSource{buf: buf, w: w, h: h, created: time.Now()}
}

func (s *infoSource) Bounds() (int, int) { return s.w, s.h }
func (s *infoSource) Close() error       { return nil }

func (s *infoSource) Capture() ([]byte, error) {
	if time.Since(s.created) > 10*time.Second {
		return nil, errors.New("hinweis-anzeige: erneut nach einer Sitzung suchen")
	}
	return s.buf, nil
}
