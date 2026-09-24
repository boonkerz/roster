package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
)

// writeIconICO legt das Anwendungssymbol als .ico ab – für Windows-Verknüpfungen.
// Seit Vista darf ein ICO-Eintrag direkt PNG-Daten enthalten, was das Format auf
// einen 22-Byte-Kopf vor dem PNG reduziert.
func writeIconICO(path string, size int) error {
	var img bytes.Buffer
	if err := png.Encode(&img, iconImage(iconBrand, size)); err != nil {
		return err
	}
	dim := byte(size) // 256 → 0 bedeutet laut Spezifikation 256
	if size >= 256 {
		dim = 0
	}
	var out bytes.Buffer
	hdr := []any{
		uint16(0), uint16(1), uint16(1), // ICONDIR: reserviert, Typ Icon, ein Eintrag
		dim, dim, byte(0), byte(0), // ICONDIRENTRY: Breite, Höhe, Palette, reserviert
		uint16(1), uint16(32), // Farbebenen, Bits pro Pixel
		uint32(img.Len()), uint32(22), // Datenlänge, Offset (6 + 16)
	}
	for _, v := range hdr {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	out.Write(img.Bytes())
	return os.WriteFile(path, out.Bytes(), 0o644)
}
