package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// previewBar rendert die Bedienleiste offscreen in ein PNG (Entwicklungs-Hilfe, um
// das Aussehen ohne echtes Fenster zu prüfen): pcinv-viewer --previewbar out.png
func previewBar(path string) error {
	if !sdl.Init(sdl.InitVideo) {
		return fmt.Errorf("sdl init: %s", sdl.GetError())
	}
	defer sdl.Quit()
	win := sdl.CreateWindow("preview", 1000, 120, sdl.WindowHidden)
	if win == nil {
		return fmt.Errorf("window: %s", sdl.GetError())
	}
	defer sdl.DestroyWindow(win)
	rn := sdl.CreateRenderer(win, "")
	if rn == nil {
		return fmt.Errorf("renderer: %s", sdl.GetError())
	}
	defer sdl.DestroyRenderer(rn)
	sdl.SetRenderDrawBlendMode(rn, sdl.BlendModeBlend)

	txt, err := newTextRenderer(rn, 15)
	if err != nil {
		return err
	}
	const W, H = 1000, 120
	target := sdl.CreateTexture(rn, sdl.PixelFormatARGB8888, sdl.TextureAccessTarget, W, H)
	if target == nil {
		return fmt.Errorf("target: %s", sdl.GetError())
	}
	sdl.SetRenderTarget(rn, target)
	// Hintergrund wie der Remote-Bereich + ein paar Bildstreifen zur Anmutung.
	sdl.SetRenderDrawColor(rn, 0x0b, 0x0e, 0x14, 0xff)
	sdl.RenderClear(rn)
	sdl.SetRenderDrawColor(rn, 0x1b, 0x22, 0x2c, 0xff)
	sdl.RenderFillRect(rn, &sdl.FRect{X: 0, Y: 60, W: W, H: 60})

	tb := newToolbar(txt)
	tb.layout(W)
	tb.draw("full", true) // Hover auf „Vollbild", Sperre aktiv (grün)

	surf := sdl.RenderReadPixels(rn, nil)
	if surf == nil {
		return fmt.Errorf("readpixels: %s", sdl.GetError())
	}
	defer sdl.DestroySurface(surf)
	img := surfaceToImage(surf)
	sdl.SetRenderTarget(rn, nil)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func surfaceToImage(s *sdl.Surface) *image.RGBA {
	w, h, pitch := int(s.W), int(s.H), int(s.Pitch)
	src := unsafe.Slice((*byte)(s.Pixels), pitch*h)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			si := y*pitch + x*4
			di := y*img.Stride + x*4
			// ARGB8888 im Speicher (LE) = B,G,R,A
			img.Pix[di] = src[si+2]
			img.Pix[di+1] = src[si+1]
			img.Pix[di+2] = src[si+0]
			img.Pix[di+3] = 0xff
		}
	}
	return img
}
