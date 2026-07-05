//go:build windows

package remote

import (
	"fmt"
	"syscall"
	"unsafe"

	"log/slog"

	"golang.org/x/sys/windows"
)

// DXGI Desktop Duplication: GPU-basierte Bildschirmaufnahme (deutlich effizienter
// als GDI-BitBlt). Reines COM-Interop über die Vtable-Indizes. Bei jedem Fehler
// wird ein Fehler zurückgegeben, sodass der Aufrufer auf GDI zurückfällt.

var (
	modD3D11              = windows.NewLazySystemDLL("d3d11.dll")
	procD3D11CreateDevice = modD3D11.NewProc("D3D11CreateDevice")
	procCreateDIBSection  = modGdi32.NewProc("CreateDIBSection")
)

var (
	iidIDXGIDevice     = windows.GUID{Data1: 0x54ec77fa, Data2: 0x1377, Data3: 0x44e6, Data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidIDXGIOutput1    = windows.GUID{Data1: 0x00cddea8, Data2: 0x939b, Data3: 0x4b83, Data4: [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc}}
	iidID3D11Texture2D = windows.GUID{Data1: 0x6f15aaf2, Data2: 0xd208, Data3: 0x4e89, Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
)

const (
	dxgiWaitTimeout = 0x887A0027 // DXGI_ERROR_WAIT_TIMEOUT
)

// comCall ruft die Methode idx aus der Vtable des COM-Objekts obj auf.
func comCall(obj uintptr, idx int, a ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, a...)...)
	return r
}

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2) // IUnknown::Release
	}
}

func comQI(obj uintptr, iid *windows.GUID) uintptr {
	var out uintptr
	comCall(obj, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out
}

type dxgiOutduplDesc struct {
	ModeWidth, ModeHeight             uint32
	RefreshNum, RefreshDen            uint32
	Format, ScanlineOrdering, Scaling uint32
	Rotation                          uint32
	DesktopImageInSystemMemory        int32
}

type dxgiFrameInfo struct {
	LastPresentTime           int64
	LastMouseUpdateTime       int64
	AccumulatedFrames         uint32
	RectsCoalesced            int32
	ProtectedContentMaskedOut int32
	PointerX, PointerY        int32
	PointerVisible            int32
	TotalMetadataBufferSize   uint32
	PointerShapeBufferSize    uint32
}

type d3dTexture2DDesc struct {
	Width, Height, MipLevels, ArraySize uint32
	Format                              uint32
	SampleCount, SampleQuality          uint32
	Usage, BindFlags, CPUAccess, Misc   uint32
}

type d3dMapped struct {
	Data                 uintptr
	RowPitch, DepthPitch uint32
}

type dxgiSource struct {
	w, h     int
	device   uintptr
	context  uintptr
	dupl     uintptr
	staging  uintptr
	memDC    uintptr
	dib      uintptr
	buf      []byte // Slice über den DIB-Speicher (Cursor wird hier einkomponiert)
	prevMask int
	winClipboard
}

func newDXGISource(log *slog.Logger, monitor int) (screenSource, error) {
	if monitor != 1 {
		// DXGI dupliziert je Output; Multi-Monitor/virtueller Desktop läuft über GDI.
		return nil, fmt.Errorf("dxgi nur für primären monitor")
	}
	var device, context uintptr
	var fl uint32
	hr, _, _ := procD3D11CreateDevice.Call(
		0, 1, 0, 0, 0, 0, 7, // adapter, HARDWARE, module, flags, FLs, numFLs, SDK_VERSION
		uintptr(unsafe.Pointer(&device)), uintptr(unsafe.Pointer(&fl)), uintptr(unsafe.Pointer(&context)))
	if uint32(hr) != 0 || device == 0 {
		return nil, fmt.Errorf("D3D11CreateDevice: 0x%x", uint32(hr))
	}
	s := &dxgiSource{device: device, context: context}
	fail := func(e error) (screenSource, error) { s.Close(); return nil, e }

	dxgiDev := comQI(device, &iidIDXGIDevice)
	if dxgiDev == 0 {
		return fail(fmt.Errorf("QI IDXGIDevice"))
	}
	defer comRelease(dxgiDev)
	var adapter uintptr
	comCall(dxgiDev, 7, uintptr(unsafe.Pointer(&adapter))) // GetAdapter
	if adapter == 0 {
		return fail(fmt.Errorf("GetAdapter"))
	}
	defer comRelease(adapter)
	var output uintptr
	if hr := comCall(adapter, 7, 0, uintptr(unsafe.Pointer(&output))); uint32(hr) != 0 || output == 0 { // EnumOutputs(0)
		return fail(fmt.Errorf("EnumOutputs: 0x%x", uint32(hr)))
	}
	defer comRelease(output)
	output1 := comQI(output, &iidIDXGIOutput1)
	if output1 == 0 {
		return fail(fmt.Errorf("QI IDXGIOutput1"))
	}
	defer comRelease(output1)
	if hr := comCall(output1, 22, device, uintptr(unsafe.Pointer(&s.dupl))); uint32(hr) != 0 || s.dupl == 0 { // DuplicateOutput
		return fail(fmt.Errorf("DuplicateOutput (evtl. keine interaktive Sitzung): 0x%x", uint32(hr)))
	}

	var dd dxgiOutduplDesc
	comCall(s.dupl, 7, uintptr(unsafe.Pointer(&dd))) // GetDesc
	s.w, s.h = int(dd.ModeWidth), int(dd.ModeHeight)
	if s.w == 0 || s.h == 0 {
		return fail(fmt.Errorf("ungültige duplication-größe"))
	}

	// Staging-Textur (CPU-lesbar) einmalig anlegen.
	td := d3dTexture2DDesc{
		Width: uint32(s.w), Height: uint32(s.h), MipLevels: 1, ArraySize: 1,
		Format: 87, SampleCount: 1, Usage: 3, CPUAccess: 0x20000, // B8G8R8A8_UNORM, STAGING, CPU_READ
	}
	if hr := comCall(device, 5, uintptr(unsafe.Pointer(&td)), 0, uintptr(unsafe.Pointer(&s.staging))); uint32(hr) != 0 || s.staging == 0 { // CreateTexture2D
		return fail(fmt.Errorf("CreateTexture2D: 0x%x", uint32(hr)))
	}

	// DIB-Section als Zielspeicher: Cursor wird per GDI hineinkomponiert, und der
	// zurückgegebene Puffer ist direkt der DIB-Speicher (keine Extra-Kopie).
	var bmi bitmapInfo
	bmi.Header = bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(s.w), Height: -int32(s.h),
		Planes: 1, Bits: 32, Compression: biRGB,
	}
	var bits uintptr
	s.dib, _, _ = procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), dibRGB, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if s.dib == 0 || bits == 0 {
		return fail(fmt.Errorf("CreateDIBSection"))
	}
	s.memDC, _, _ = procCreateCompatDC.Call(0)
	procSelectObject.Call(s.memDC, s.dib)
	s.buf = unsafe.Slice((*byte)(unsafe.Pointer(bits)), s.w*s.h*4)

	log.Info("dxgi-bildschirmaufnahme bereit", "size", fmt.Sprintf("%dx%d", s.w, s.h))
	return s, nil
}

func (s *dxgiSource) Bounds() (int, int) { return s.w, s.h }

func (s *dxgiSource) Capture() ([]byte, error) {
	var fi dxgiFrameInfo
	var resource uintptr
	hr := comCall(s.dupl, 8, 200, uintptr(unsafe.Pointer(&fi)), uintptr(unsafe.Pointer(&resource))) // AcquireNextFrame
	if uint32(hr) == dxgiWaitTimeout {
		return s.buf, nil // kein neuer Frame -> letztes Bild
	}
	if uint32(hr) != 0 || resource == 0 {
		return nil, fmt.Errorf("AcquireNextFrame: 0x%x", uint32(hr))
	}
	texture := comQI(resource, &iidID3D11Texture2D)
	if texture != 0 {
		comCall(s.context, 47, s.staging, texture) // CopyResource(dst,src)
		var m d3dMapped
		if hr := comCall(s.context, 14, s.staging, 0, 1, 0, uintptr(unsafe.Pointer(&m))); uint32(hr) == 0 && m.Data != 0 { // Map (READ)
			row := s.w * 4
			for y := 0; y < s.h; y++ {
				src := unsafe.Slice((*byte)(unsafe.Pointer(m.Data+uintptr(y)*uintptr(m.RowPitch))), row)
				copy(s.buf[y*row:], src)
			}
			comCall(s.context, 15, s.staging, 0) // Unmap
		}
		comRelease(texture)
	}
	comRelease(resource)
	comCall(s.dupl, 14)       // ReleaseFrame
	drawCursor(s.memDC, 0, 0) // Cursor in den DIB-Speicher (= s.buf) einzeichnen
	return s.buf, nil
}

func (s *dxgiSource) Pointer(mask, x, y int)       { pointerEvent(&s.prevMask, mask, x, y, 0, 0) }
func (s *dxgiSource) Key(down bool, keysym uint32) { keyEvent(down, keysym) }

func (s *dxgiSource) Close() error {
	comRelease(s.dupl)
	comRelease(s.staging)
	comRelease(s.context)
	comRelease(s.device)
	if s.memDC != 0 {
		procDeleteDC.Call(s.memDC)
	}
	if s.dib != 0 {
		procDeleteObject.Call(s.dib)
	}
	return nil
}

// newCaptureSource bevorzugt DXGI (GPU) und fällt auf GDI zurück.
func newCaptureSource(log *slog.Logger, monitor int) (screenSource, error) {
	if s, err := newDXGISource(log, monitor); err == nil {
		return s, nil
	} else {
		log.Info("dxgi nicht verfügbar – GDI-Aufnahme", "err", err)
	}
	return newGDISource(log, monitor)
}
