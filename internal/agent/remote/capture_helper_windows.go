//go:build windows

package remote

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"

	"log/slog"

	"golang.org/x/sys/windows"
)

// Windows-Dienste laufen in Session 0 und sehen den Desktop des angemeldeten
// Nutzers nicht (Session-0-Isolation → GDI liefert Schwarz). Lösung: der Dienst
// startet den Agent erneut im versteckten Modus "__capture" IN der aktiven
// Nutzer-Session (CreateProcessAsUser) und liest dessen Frames über eine Pipe.

// newScreenSource bevorzugt den Nutzer-Session-Helfer (funktioniert als Dienst,
// ohne Zutun des Nutzers); Fallback ist die direkte GDI-Aufnahme (z.B. wenn der
// Agent bereits interaktiv in der Sitzung läuft).
func newScreenSource(log *slog.Logger) (screenSource, error) {
	if s, err := startCaptureHelper(log); err == nil {
		return s, nil
	} else {
		log.Info("nutzer-session-helfer nicht verfügbar – direkte Aufnahme", "err", err)
	}
	return newCaptureSource(log)
}

// RunCaptureHelper ist der __capture-Modus: läuft in der Nutzer-Session, nimmt den
// Bildschirm per GDI auf und liefert Frames über stdin/stdout an den Dienst.
// Protokoll: 8-Byte-Header (w,h little-endian), dann je 1 Anfrage-Byte auf stdin
// ein Vollbild (w*h*4 Bytes) auf stdout.
func RunCaptureHelper() {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	src, err := newCaptureSource(discard)
	if err != nil {
		os.Exit(2)
	}
	defer src.Close()
	in, _ := src.(inputSink)
	w, h := src.Bounds()
	hdr := make([]byte, 8)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(w))
	binary.LittleEndian.PutUint32(hdr[4:], uint32(h))
	if _, err := os.Stdout.Write(hdr); err != nil {
		return
	}
	cmd := make([]byte, 1)
	arg := make([]byte, 5)
	for {
		if _, err := io.ReadFull(os.Stdin, cmd); err != nil {
			return
		}
		switch cmd[0] {
		case cmdCapture:
			px, err := src.Capture()
			if err != nil {
				return
			}
			if _, err := os.Stdout.Write(px[:w*h*4]); err != nil {
				return
			}
		case cmdPointer: // mask(1) + x(2 LE) + y(2 LE)
			if _, err := io.ReadFull(os.Stdin, arg); err != nil {
				return
			}
			if in != nil {
				in.Pointer(int(arg[0]), int(binary.LittleEndian.Uint16(arg[1:])), int(binary.LittleEndian.Uint16(arg[3:])))
			}
		case cmdKey: // down(1) + keysym(4 LE)
			if _, err := io.ReadFull(os.Stdin, arg); err != nil {
				return
			}
			if in != nil {
				in.Key(arg[0] != 0, binary.LittleEndian.Uint32(arg[1:]))
			}
		default:
			return
		}
	}
}

const (
	cmdCapture = 1
	cmdPointer = 2
	cmdKey     = 3
)

type helperSource struct {
	proc    windows.Handle
	token   windows.Token
	env     *uint16
	stdinW  *os.File
	stdoutR *os.File
	w, h    int
	buf     []byte
}

func startCaptureHelper(log *slog.Logger) (screenSource, error) {
	sess := windows.WTSGetActiveConsoleSessionId()
	if sess == 0xFFFFFFFF {
		return nil, fmt.Errorf("keine aktive konsolen-sitzung")
	}
	var token windows.Token
	if err := windows.WTSQueryUserToken(sess, &token); err != nil {
		return nil, fmt.Errorf("WTSQueryUserToken (Agent muss SYSTEM-Dienst sein): %w", err)
	}

	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))

	var stdoutR, stdoutW windows.Handle // Kind schreibt stdoutW, Eltern lesen stdoutR
	if err := windows.CreatePipe(&stdoutR, &stdoutW, sa, 0); err != nil {
		token.Close()
		return nil, err
	}
	var stdinR, stdinW windows.Handle // Eltern schreiben stdinW, Kind liest stdinR
	if err := windows.CreatePipe(&stdinR, &stdinW, sa, 0); err != nil {
		token.Close()
		windows.CloseHandle(stdoutR)
		windows.CloseHandle(stdoutW)
		return nil, err
	}
	// Eltern-Enden nicht an das Kind vererben.
	windows.SetHandleInformation(stdoutR, windows.HANDLE_FLAG_INHERIT, 0)
	windows.SetHandleInformation(stdinW, windows.HANDLE_FLAG_INHERIT, 0)

	exe, err := os.Executable()
	if err != nil {
		token.Close()
		windows.CloseHandle(stdoutR)
		windows.CloseHandle(stdoutW)
		windows.CloseHandle(stdinR)
		windows.CloseHandle(stdinW)
		return nil, err
	}

	var env *uint16
	_ = windows.CreateEnvironmentBlock(&env, token, false)

	cmdline, _ := windows.UTF16PtrFromString(fmt.Sprintf(`"%s" __capture`, exe))
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	si := windows.StartupInfo{
		Desktop:   desktop,
		Flags:     windows.STARTF_USESTDHANDLES,
		StdInput:  stdinR,
		StdOutput: stdoutW,
		StdErr:    stdoutW,
	}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NO_WINDOW)
	err = windows.CreateProcessAsUser(token, nil, cmdline, nil, nil, true, flags, env, nil, &si, &pi)
	// Kind-Enden im Elternprozess schließen (unabhängig vom Ergebnis).
	windows.CloseHandle(stdinR)
	windows.CloseHandle(stdoutW)
	if err != nil {
		windows.CloseHandle(stdoutR)
		windows.CloseHandle(stdinW)
		if env != nil {
			windows.DestroyEnvironmentBlock(env)
		}
		token.Close()
		return nil, fmt.Errorf("CreateProcessAsUser: %w", err)
	}
	windows.CloseHandle(pi.Thread)

	hs := &helperSource{
		proc:    pi.Process,
		token:   token,
		env:     env,
		stdinW:  os.NewFile(uintptr(stdinW), "capture-stdin"),
		stdoutR: os.NewFile(uintptr(stdoutR), "capture-stdout"),
	}
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(hs.stdoutR, hdr); err != nil {
		hs.Close()
		return nil, fmt.Errorf("helfer-header: %w", err)
	}
	hs.w = int(binary.LittleEndian.Uint32(hdr[0:]))
	hs.h = int(binary.LittleEndian.Uint32(hdr[4:]))
	if hs.w == 0 || hs.h == 0 {
		hs.Close()
		return nil, fmt.Errorf("helfer lieferte ungültige größe")
	}
	hs.buf = make([]byte, hs.w*hs.h*4)
	log.Info("aufnahme-helfer in nutzer-session", "size", fmt.Sprintf("%dx%d", hs.w, hs.h), "session", sess)
	return hs, nil
}

func (h *helperSource) Bounds() (int, int) { return h.w, h.h }

func (h *helperSource) Capture() ([]byte, error) {
	if _, err := h.stdinW.Write([]byte{cmdCapture}); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(h.stdoutR, h.buf); err != nil {
		return nil, err
	}
	return h.buf, nil
}

// Pointer/Key reichen die Eingabe an den Helfer weiter (der SendInput ausführt).
func (h *helperSource) Pointer(mask, x, y int) {
	b := []byte{cmdPointer, byte(mask), 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(b[2:], uint16(x))
	binary.LittleEndian.PutUint16(b[4:], uint16(y))
	_, _ = h.stdinW.Write(b)
}

func (h *helperSource) Key(down bool, keysym uint32) {
	b := []byte{cmdKey, 0, 0, 0, 0, 0}
	if down {
		b[1] = 1
	}
	binary.LittleEndian.PutUint32(b[2:], keysym)
	_, _ = h.stdinW.Write(b)
}

func (h *helperSource) Close() error {
	if h.stdinW != nil {
		h.stdinW.Close()
	}
	if h.stdoutR != nil {
		h.stdoutR.Close()
	}
	if h.proc != 0 {
		windows.TerminateProcess(h.proc, 0)
		windows.CloseHandle(h.proc)
	}
	if h.env != nil {
		windows.DestroyEnvironmentBlock(h.env)
	}
	if h.token != 0 {
		h.token.Close()
	}
	return nil
}
