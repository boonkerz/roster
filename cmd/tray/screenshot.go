package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// saveScreenshot zeichnet einen Frame in eine Textur mit der Basis-Fenstergröße
// (mal Skalierung) und legt ihn als PNG ab (--screenshot). So ist das Bild
// unabhängig davon, welche Größe der Compositor dem Fenster tatsächlich gibt
// (Tiling-WMs maximieren gern) – wichtig für reproduzierbare Doku-Screenshots.
func (a *app) saveScreenshot(path string) error {
	tw, th := int32(baseWinW*a.scale), int32(baseWinH*a.scale)
	target := sdl.CreateTexture(a.rn, sdl.PixelFormatRGBA8888, sdl.TextureAccessTarget, tw, th)
	if target == nil {
		return fmt.Errorf("render-ziel: %s", sdl.GetError())
	}
	defer sdl.DestroyTexture(target)
	if !sdl.SetRenderTarget(a.rn, target) {
		return fmt.Errorf("render-ziel setzen: %s", sdl.GetError())
	}
	a.shotW, a.shotH = tw, th
	a.draw() // ohne Present – RenderReadPixels liest das Render-Ziel
	a.shotW, a.shotH = 0, 0
	surf := sdl.RenderReadPixels(a.rn, nil)
	sdl.SetRenderTarget(a.rn, nil)
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
