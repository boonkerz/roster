package main

import (
	"image"
	"image/color"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// textRenderer rastert Strings mit der eingebetteten Go-Font (antialiased, reines Go)
// und lädt sie als SDL-Texturen hoch (gecacht). Weiß gerendert, Einfärbung per
// ColorMod – so reicht eine Textur je String für beliebige Farben.
type textRenderer struct {
	renderer *sdl.Renderer
	face     font.Face
	ascent   int
	height   int
	cache    map[string]*textTex
}

type textTex struct {
	tex  *sdl.Texture
	w, h int32
}

func newTextRenderer(renderer *sdl.Renderer, px float64) (*textRenderer, error) {
	f, err := opentype.Parse(gomedium.TTF)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	m := face.Metrics()
	return &textRenderer{
		renderer: renderer, face: face,
		ascent: m.Ascent.Ceil(), height: (m.Ascent + m.Descent).Ceil(),
		cache: map[string]*textTex{},
	}, nil
}

func (t *textRenderer) get(s string) *textTex {
	if tt, ok := t.cache[s]; ok {
		return tt
	}
	d := &font.Drawer{Face: t.face}
	w := d.MeasureString(s).Ceil()
	if w < 1 {
		w = 1
	}
	h := t.height
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	d = &font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: t.face, Dot: fixed.P(0, t.ascent)}
	d.DrawString(s)
	// Go-Font liefert alpha-premultipliziertes Weiß (R=G=B=A); für SDLs Straight-Alpha-
	// Blend RGB auf 255 setzen und nur Alpha behalten -> ColorMod färbt korrekt.
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = 0xff, 0xff, 0xff
	}
	tex := sdl.CreateTexture(t.renderer, sdl.PixelFormatABGR8888, sdl.TextureAccessStatic, int32(w), int32(h))
	sdl.UpdateTexture(tex, nil, unsafe.Pointer(&img.Pix[0]), int32(img.Stride))
	sdl.SetTextureBlendMode(tex, sdl.BlendModeBlend)
	sdl.SetTextureScaleMode(tex, sdl.ScaleModeLinear)
	tt := &textTex{tex: tex, w: int32(w), h: int32(h)}
	t.cache[s] = tt
	return tt
}

// draw zeichnet s linksbündig mit Oberkante bei (x,y) in Farbe (r,g,b) und liefert die Breite.
func (t *textRenderer) draw(s string, x, y float32, r, g, b uint8) float32 {
	tt := t.get(s)
	sdl.SetTextureColorMod(tt.tex, r, g, b)
	dst := sdl.FRect{X: x, Y: y, W: float32(tt.w), H: float32(tt.h)}
	sdl.RenderTexture(t.renderer, tt.tex, nil, &dst)
	return float32(tt.w)
}

func (t *textRenderer) width(s string) float32 { return float32(t.get(s).w) }
func (t *textRenderer) lineH() float32         { return float32(t.height) }
