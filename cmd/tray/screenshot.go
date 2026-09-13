package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// saveScreenshot liest den zuletzt gezeichneten Frame zurück und legt ihn als PNG ab
// (--screenshot). Gedacht für die Dokumentation und zum Prüfen des Layouts ohne
// sichtbares Fenster.
func (a *app) saveScreenshot(path string) error {
	surf := sdl.RenderReadPixels(a.rn, nil)
	if surf == nil {
		return fmt.Errorf("frame lesen: %s", sdl.GetError())
	}
	defer sdl.DestroySurface(surf)
	conv := sdl.ConvertSurface(surf, sdl.PixelFormatABGR8888)
	if conv == nil {
		return fmt.Errorf("frame konvertieren: %s", sdl.GetError())
	}
	defer sdl.DestroySurface(conv)

	w, h := int(conv.W), int(conv.H)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	src := unsafe.Slice((*byte)(conv.Pixels), int(conv.Pitch)*h)
	for y := 0; y < h; y++ {
		copy(img.Pix[y*img.Stride:(y+1)*img.Stride], src[y*int(conv.Pitch):y*int(conv.Pitch)+w*4])
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
