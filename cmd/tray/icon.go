package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// iconState bestimmt die Farbe des Tray-Symbols: auf einen Blick soll erkennbar
// sein, ob etwas rot ist.
type iconState int

const (
	iconIdle  iconState = iota // nicht angemeldet / keine Daten
	iconOK                     // alle Checks und Tasks grün
	iconWarn                   // einzelne Geräte offline
	iconAlert                  // fehlgeschlagene Checks oder Tasks
	iconBrand                  // statusneutral – Anwendungssymbol im Desktop-Menü
)

// makeIcon zeichnet das Tray-Symbol (64×64) als SDL-Surface.
func makeIcon(st iconState) *sdl.Surface { return surfaceFromImage(iconImage(st, 64)) }

// iconImage zeichnet das Symbol: abgerundetes Quadrat in Statusfarbe mit weißem
// „R". Rein in Go gerastert (kein Bildformat, keine Fremddatei) – dieselbe Grafik
// dient als Tray-Symbol und als Anwendungssymbol im Desktop-Menü.
func iconImage(st iconState, size int) *image.RGBA {
	var bg color.RGBA
	switch st {
	case iconOK:
		bg = color.RGBA{0x2e, 0x7d, 0x32, 0xff}
	case iconWarn:
		bg = color.RGBA{0x9a, 0x6b, 0x1f, 0xff}
	case iconAlert:
		bg = color.RGBA{0x9c, 0x2b, 0x2b, 0xff}
	case iconBrand:
		bg = color.RGBA{0x2f, 0x5a, 0x8f, 0xff}
	default:
		bg = color.RGBA{0x45, 0x4d, 0x5a, 0xff}
	}

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Abgerundetes Quadrat (zeilenweise, wie sdlui.FillRound – hier auf Pixelebene).
	fsize := float64(size)
	rad := fsize * 14 / 64
	for y := 0; y < size; y++ {
		fy := float64(y) + 0.5
		inset := 0.0
		d := -1.0
		if fy < rad {
			d = rad - fy
		} else if fy > fsize-rad {
			d = fy - (fsize - rad)
		}
		if d >= 0 {
			inset = rad - math.Sqrt(rad*rad-d*d)
		}
		x0, x1 := int(math.Round(inset)), size-int(math.Round(inset))
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, bg)
		}
	}

	// „R" mittig in Weiß.
	if f, err := opentype.Parse(gomedium.TTF); err == nil {
		if face, ferr := opentype.NewFace(f, &opentype.FaceOptions{Size: fsize * 44 / 64, DPI: 72, Hinting: font.HintingFull}); ferr == nil {
			defer face.Close()
			d := &font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: face}
			w := d.MeasureString("R").Ceil()
			m := face.Metrics()
			d.Dot = fixed.P((size-w)/2, (size+m.Ascent.Ceil()-m.Descent.Ceil())/2)
			d.DrawString("R")
		}
	}
	return img
}

// writeIconPNG legt das Anwendungssymbol als PNG ab (für Paketierung und den
// Desktop-Eintrag – siehe deploy/linux).
func writeIconPNG(path string, size int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, iconImage(iconBrand, size))
}

// surfaceFromImage kopiert ein RGBA-Bild in eine SDL-Surface (ABGR8888 = R,G,B,A
// im Speicher, passt zu image.RGBA).
func surfaceFromImage(img *image.RGBA) *sdl.Surface {
	b := img.Bounds()
	surf := sdl.CreateSurface(int32(b.Dx()), int32(b.Dy()), sdl.PixelFormatABGR8888)
	if surf == nil || surf.Pixels == nil {
		return nil
	}
	dst := unsafe.Slice((*byte)(surf.Pixels), int(surf.Pitch)*b.Dy())
	for y := 0; y < b.Dy(); y++ {
		copy(dst[y*int(surf.Pitch):], img.Pix[y*img.Stride:y*img.Stride+b.Dx()*4])
	}
	return surf
}
