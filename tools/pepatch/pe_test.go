package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"testing"
)

// fakePE baut einen minimalen PE32+-Kopf ohne Sektionen, wie ihn der Go-Linker
// in den Versionsfeldern hinterlässt (10.0).
func fakePE(t *testing.T) []byte {
	t.Helper()
	buf := make([]byte, 0x40+4+20+240)
	buf[0], buf[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(buf[0x3c:], 0x40)
	copy(buf[0x40:], "PE\x00\x00")
	coff := buf[0x44:]
	binary.LittleEndian.PutUint16(coff[0:], 0x8664) // Machine amd64
	binary.LittleEndian.PutUint16(coff[16:], 240)   // SizeOfOptionalHeader
	opt := buf[0x44+20:]
	binary.LittleEndian.PutUint16(opt[0:], peMagic64)
	binary.LittleEndian.PutUint16(opt[40:], 10)  // MajorOperatingSystemVersion
	binary.LittleEndian.PutUint16(opt[48:], 10)  // MajorSubsystemVersion
	binary.LittleEndian.PutUint16(opt[68:], 3)   // Subsystem CUI
	binary.LittleEndian.PutUint32(opt[108:], 16) // NumberOfRvaAndSizes
	return buf
}

func TestPatchMinVersion(t *testing.T) {
	data := fakePE(t)
	changed, err := patchMinVersion(data, 6, 1)
	if err != nil || !changed {
		t.Fatalf("patch: changed=%v err=%v", changed, err)
	}
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("debug/pe liest gepatchte Datei nicht: %v", err)
	}
	oh := f.OptionalHeader.(*pe.OptionalHeader64)
	if oh.MajorOperatingSystemVersion != 6 || oh.MinorOperatingSystemVersion != 1 || oh.MajorSubsystemVersion != 6 || oh.MinorSubsystemVersion != 1 {
		t.Fatalf("Versionen nicht gesetzt: OS %d.%d Subsystem %d.%d", oh.MajorOperatingSystemVersion, oh.MinorOperatingSystemVersion, oh.MajorSubsystemVersion, oh.MinorSubsystemVersion)
	}
	if oh.Subsystem != 3 || f.Machine != 0x8664 {
		t.Fatalf("andere Felder verändert: subsystem=%d machine=%#x", oh.Subsystem, f.Machine)
	}
	// Zweiter Lauf ist idempotent.
	if changed, err := patchMinVersion(data, 6, 1); err != nil || changed {
		t.Fatalf("zweiter Lauf: changed=%v err=%v", changed, err)
	}
	if maj, min, _ := minVersion(data); maj != 6 || min != 1 {
		t.Fatalf("minVersion = %d.%d", maj, min)
	}
}

func TestPatchRejectsGarbage(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("hello"), bytes.Repeat([]byte{0}, 4096)} {
		if _, err := patchMinVersion(in, 6, 1); err == nil {
			t.Fatalf("%q: Fehler erwartet", in)
		}
	}
	bad := fakePE(t)
	binary.LittleEndian.PutUint16(bad[0x44+20:], 0x123) // falsches Magic
	if _, err := patchMinVersion(bad, 6, 1); err == nil {
		t.Fatal("falsches Magic muss abgelehnt werden")
	}
}
