//go:build windows

package remote

import (
	"syscall"
	"unsafe"
)

var procEnumDisplayMonitors = modUser32.NewProc("EnumDisplayMonitors")

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func sysMetric(i int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(i))
	return int(int32(r))
}

// monitorRects liefert die Rechtecke aller Monitore; der primäre (0,0) wird nach
// vorne sortiert, damit „Monitor 1" der primäre ist.
func monitorRects() []winRect {
	var rects []winRect
	cb := syscall.NewCallback(func(_ uintptr, _ uintptr, lprc uintptr, _ uintptr) uintptr {
		rects = append(rects, *(*winRect)(unsafe.Pointer(lprc)))
		return 1
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	for i, r := range rects {
		if r.Left == 0 && r.Top == 0 {
			rects[0], rects[i] = rects[i], rects[0]
			break
		}
	}
	return rects
}

// captureRect liefert den aufzunehmenden Bereich für die Monitor-Auswahl:
// 0 = alle (virtueller Desktop), 1..N = einzelner Monitor (Fallback: primär).
func captureRect(monitor int) (x, y, w, h int) {
	if monitor == 0 {
		return sysMetric(smXVirtualScreen), sysMetric(smYVirtualScreen),
			sysMetric(smCXVirtualScreen), sysMetric(smCYVirtualScreen)
	}
	if monitor >= 1 {
		if rects := monitorRects(); monitor <= len(rects) {
			r := rects[monitor-1]
			return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)
		}
	}
	return 0, 0, sysMetric(smCXScreen), sysMetric(smCYScreen) // primär
}
