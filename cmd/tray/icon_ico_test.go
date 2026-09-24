package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteIconICO(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.ico")
	if err := writeIconICO(p, 256); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 22 || !bytes.Equal(b[:6], []byte{0, 0, 1, 0, 1, 0}) {
		t.Fatalf("ungültiger ICONDIR: % x", b[:6])
	}
	if b[6] != 0 || b[7] != 0 {
		t.Fatalf("256px muss als 0 kodiert sein, got %d×%d", b[6], b[7])
	}
	n := binary.LittleEndian.Uint32(b[14:18])
	off := binary.LittleEndian.Uint32(b[18:22])
	if off != 22 || int(off+n) != len(b) {
		t.Fatalf("Offset/Länge passen nicht: off=%d n=%d len=%d", off, n, len(b))
	}
	img, err := png.Decode(bytes.NewReader(b[off:]))
	if err != nil {
		t.Fatalf("PNG-Nutzlast: %v", err)
	}
	if img.Bounds().Dx() != 256 {
		t.Fatalf("Breite %d", img.Bounds().Dx())
	}
}
