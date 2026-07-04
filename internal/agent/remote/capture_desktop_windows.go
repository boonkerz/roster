//go:build windows

package remote

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Damit die Fernsteuerung auch am Anmeldebildschirm, bei gesperrtem Rechner und bei
// UAC-Abfragen (sicherer Desktop) funktioniert, muss der Aufnahme-Helfer als SYSTEM
// laufen und dem jeweils aktiven Eingabe-Desktop folgen.

var (
	procOpenInputDesktop          = modUser32.NewProc("OpenInputDesktop")
	procSetThreadDesktop          = modUser32.NewProc("SetThreadDesktop")
	procCloseDesktop              = modUser32.NewProc("CloseDesktop")
	procGetUserObjectInformationW = modUser32.NewProc("GetUserObjectInformationW")
)

const (
	uoiName    = 2
	genericAll = 0x10000000
)

// systemSessionToken liefert ein primäres SYSTEM-Token, das auf die angegebene
// (interaktive) Session gesetzt ist. Voraussetzung: der Aufrufer ist SYSTEM
// (SeTcbPrivilege) – d.h. der Agent läuft als Dienst.
func systemSessionToken(sessionID uint32) (windows.Token, error) {
	var proc windows.Token
	const adjustSession = 0x0100 // TOKEN_ADJUST_SESSIONID
	access := uint32(windows.TOKEN_DUPLICATE | windows.TOKEN_QUERY | windows.TOKEN_ASSIGN_PRIMARY |
		windows.TOKEN_ADJUST_DEFAULT | adjustSession)
	if err := windows.OpenProcessToken(windows.CurrentProcess(), access, &proc); err != nil {
		return 0, err
	}
	defer proc.Close()
	var dup windows.Token
	if err := windows.DuplicateTokenEx(proc, windows.MAXIMUM_ALLOWED, nil,
		windows.SecurityIdentification, windows.TokenPrimary, &dup); err != nil {
		return 0, err
	}
	sid := sessionID
	if err := windows.SetTokenInformation(dup, windows.TokenSessionId, (*byte)(unsafe.Pointer(&sid)), 4); err != nil {
		dup.Close()
		return 0, err
	}
	return dup, nil
}

// desktopFollower hält das aktuell gesetzte Desktop-Handle und wechselt bei Bedarf
// auf den aktiven Eingabe-Desktop. Muss immer vom selben (gesperrten) OS-Thread
// aufgerufen werden.
type desktopFollower struct {
	handle uintptr
	name   string
}

// follow setzt den Thread auf den aktuellen Eingabe-Desktop und liefert dessen
// Namen sowie ob sich der Desktop geändert hat.
func (d *desktopFollower) follow() (string, bool) {
	h, _, _ := procOpenInputDesktop.Call(0, 0, genericAll)
	if h == 0 {
		return d.name, false
	}
	var buf [256]uint16
	var needed uint32
	procGetUserObjectInformationW.Call(h, uoiName, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed)))
	name := windows.UTF16ToString(buf[:])
	if name == d.name && d.handle != 0 {
		procCloseDesktop.Call(h) // gleicher Desktop -> neues Handle nicht behalten
		return name, false
	}
	procSetThreadDesktop.Call(h)
	if d.handle != 0 {
		procCloseDesktop.Call(d.handle)
	}
	d.handle = h
	d.name = name
	return name, true
}

func (d *desktopFollower) close() {
	if d.handle != 0 {
		procCloseDesktop.Call(d.handle)
		d.handle = 0
	}
}
