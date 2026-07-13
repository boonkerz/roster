//go:build windows

package collect

import (
	"encoding/json"
	"fmt"
	"sort"
	"syscall"
	"unsafe"
)

// Bildschirmauflösung des Primärdisplays enumerieren/setzen (Win32). Nützlich v. a.
// für VMs (z. B. Proxmox mit VirtIO-GPU/QXL), wo die Auflösung nach Treiber-
// Installation umgeschaltet werden soll.

var (
	modUser32res                 = syscall.NewLazyDLL("user32.dll")
	procEnumDisplaySettingsW     = modUser32res.NewProc("EnumDisplaySettingsW")
	procChangeDisplaySettingsExW = modUser32res.NewProc("ChangeDisplaySettingsExW")
)

const (
	enumCurrentSettings  = 0xFFFFFFFF
	dmPelsWidthFlag      = 0x00080000
	dmPelsHeightFlag     = 0x00100000
	cdsUpdateRegistry    = 0x00000001
	dispChangeSuccessful = 0
)

// devmodeW spiegelt DEVMODEW (Display-Variante der Union). Reihenfolge/Größe müssen
// exakt zur Win32-Struktur passen.
type devmodeW struct {
	dmDeviceName         [32]uint16
	dmSpecVersion        uint16
	dmDriverVersion      uint16
	dmSize               uint16
	dmDriverExtra        uint16
	dmFields             uint32
	dmPositionX          int32
	dmPositionY          int32
	dmDisplayOrientation uint32
	dmDisplayFixedOutput uint32
	dmColor              int16
	dmDuplex             int16
	dmYResolution        int16
	dmTTOption           int16
	dmCollate            int16
	dmFormName           [32]uint16
	dmLogPixels          uint16
	dmBitsPerPel         uint32
	dmPelsWidth          uint32
	dmPelsHeight         uint32
	dmDisplayFlags       uint32
	dmDisplayFrequency   uint32
	dmICMMethod          uint32
	dmICMIntent          uint32
	dmMediaType          uint32
	dmDitherType         uint32
	dmReserved1          uint32
	dmReserved2          uint32
	dmPanningWidth       uint32
	dmPanningHeight      uint32
}

func enumDisplaySettings(mode uint32, dm *devmodeW) bool {
	dm.dmSize = uint16(unsafe.Sizeof(*dm))
	r, _, _ := procEnumDisplaySettingsW.Call(0, uintptr(mode), uintptr(unsafe.Pointer(dm)))
	return r != 0
}

type resMode struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ListResolutions liefert die verfügbaren Auflösungen + die aktuelle als JSON.
func ListResolutions() string {
	out := struct {
		Current *resMode  `json:"current,omitempty"`
		Modes   []resMode `json:"modes"`
	}{Modes: []resMode{}}

	var cur devmodeW
	if enumDisplaySettings(enumCurrentSettings, &cur) {
		out.Current = &resMode{Width: int(cur.dmPelsWidth), Height: int(cur.dmPelsHeight)}
	}

	seen := map[[2]int]bool{}
	var dm devmodeW
	for i := uint32(0); enumDisplaySettings(i, &dm); i++ {
		w, h := int(dm.dmPelsWidth), int(dm.dmPelsHeight)
		if w < 640 || dm.dmBitsPerPel < 16 {
			continue
		}
		k := [2]int{w, h}
		if seen[k] {
			continue
		}
		seen[k] = true
		out.Modes = append(out.Modes, resMode{Width: w, Height: h})
	}
	sort.Slice(out.Modes, func(a, b int) bool {
		if out.Modes[a].Width != out.Modes[b].Width {
			return out.Modes[a].Width > out.Modes[b].Width
		}
		return out.Modes[a].Height > out.Modes[b].Height
	})

	b, _ := json.Marshal(out)
	return string(b)
}

// SetResolution setzt die Auflösung des Primärdisplays (dauerhaft, Registry).
func SetResolution(w, h int) (int, string) {
	if w <= 0 || h <= 0 {
		return 1, "ungültige Auflösung"
	}
	var dm devmodeW
	if !enumDisplaySettings(enumCurrentSettings, &dm) {
		return 1, "aktuelle Anzeigeeinstellungen nicht lesbar"
	}
	dm.dmPelsWidth = uint32(w)
	dm.dmPelsHeight = uint32(h)
	dm.dmFields = dmPelsWidthFlag | dmPelsHeightFlag
	r, _, _ := procChangeDisplaySettingsExW.Call(0, uintptr(unsafe.Pointer(&dm)), 0, cdsUpdateRegistry, 0)
	if int32(r) == dispChangeSuccessful {
		return 0, fmt.Sprintf("Auflösung auf %dx%d gesetzt", w, h)
	}
	return 1, fmt.Sprintf("Auflösung %dx%d abgelehnt (Code %d) – wird vom Anzeigetreiber nicht angeboten", w, h, int32(r))
}
