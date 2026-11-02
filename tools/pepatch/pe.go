// Package main (pepatch) setzt im PE-Kopf einer Windows-EXE die eingetragene
// Mindest-Betriebssystemversion herab.
//
// Der Go-Linker schreibt seit Go 1.23 fest „10.0“ in OperatingSystemVersion und
// SubsystemVersion. Windows 7/8/8.1 und Server 2008 R2 bis 2012 R2 vergleichen
// die SubsystemVersion mit ihrer eigenen und lehnen die Datei dann vor dem Start
// mit „keine gültige Win32-Anwendung“ ab – obwohl die Go-Laufzeit dort läuft
// (sie braucht nur Vista-/Win7-APIs und hat für Versionen < 10 eigene Pfade).
// Nach dem Bauen wird der Wert daher auf 6.1 (Windows 7 / Server 2008 R2) gesetzt.
package main

import (
	"encoding/binary"
	"fmt"
)

const (
	peMagic32     = 0x10b
	peMagic64     = 0x20b
	offOSVersion  = 40 // Major/MinorOperatingSystemVersion im Optional Header
	offSubsystemV = 48 // Major/MinorSubsystemVersion im Optional Header
)

// patchMinVersion trägt major.minor als Betriebssystem- und Subsystem-Mindestversion
// ein und meldet, ob sich dadurch etwas geändert hat.
func patchMinVersion(data []byte, major, minor uint16) (changed bool, err error) {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return false, fmt.Errorf("kein MZ-Header")
	}
	pe := int(binary.LittleEndian.Uint32(data[0x3c:]))
	if pe <= 0 || pe+24+52 > len(data) || string(data[pe:pe+4]) != "PE\x00\x00" {
		return false, fmt.Errorf("kein PE-Header")
	}
	optSize := int(binary.LittleEndian.Uint16(data[pe+20:]))
	opt := pe + 24
	if optSize < 52 || opt+optSize > len(data) {
		return false, fmt.Errorf("Optional Header zu kurz (%d Byte)", optSize)
	}
	if m := binary.LittleEndian.Uint16(data[opt:]); m != peMagic32 && m != peMagic64 {
		return false, fmt.Errorf("unbekanntes Optional-Header-Magic %#x", m)
	}
	for _, off := range []int{offOSVersion, offSubsystemV} {
		p := opt + off
		if binary.LittleEndian.Uint16(data[p:]) != major || binary.LittleEndian.Uint16(data[p+2:]) != minor {
			binary.LittleEndian.PutUint16(data[p:], major)
			binary.LittleEndian.PutUint16(data[p+2:], minor)
			changed = true
		}
	}
	return changed, nil
}

// minVersion liest die eingetragene Subsystem-Mindestversion (nur für Ausgabe/Tests).
func minVersion(data []byte) (major, minor uint16, err error) {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return 0, 0, fmt.Errorf("kein MZ-Header")
	}
	pe := int(binary.LittleEndian.Uint32(data[0x3c:]))
	if pe <= 0 || pe+24+52 > len(data) {
		return 0, 0, fmt.Errorf("kein PE-Header")
	}
	opt := pe + 24
	return binary.LittleEndian.Uint16(data[opt+offSubsystemV:]), binary.LittleEndian.Uint16(data[opt+offSubsystemV+2:]), nil
}
