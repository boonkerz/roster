//go:build windows

package remote

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"runtime"
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
func newScreenSource(log *slog.Logger, monitor int) (screenSource, error) {
	if s, err := startCaptureHelper(log, monitor); err == nil {
		return s, nil
	} else if inSession0() {
		// Dienst ohne aufnehmbare Nutzer-Session (z.B. niemand angemeldet /
		// Login-Screen). Fehler zurückgeben, damit die resiliente Quelle wartet
		// und erneut versucht, statt nutzlos die (schwarze) Session 0 aufzunehmen.
		return nil, fmt.Errorf("keine aufnehmbare sitzung: %w", err)
	} else {
		log.Info("nutzer-session-helfer nicht verfügbar – direkte Aufnahme", "err", err)
	}
	return newCaptureSource(log, monitor)
}

var procProcessIdToSessionId = modKernel32.NewProc("ProcessIdToSessionId")

// inSession0 meldet, ob der Agent selbst in Session 0 läuft (also als Dienst).
func inSession0() bool {
	var sid uint32
	r, _, _ := procProcessIdToSessionId.Call(uintptr(windows.GetCurrentProcessId()), uintptr(unsafe.Pointer(&sid)))
	return r != 0 && sid == 0
}

// RunCaptureHelper ist der __capture-Modus: läuft als SYSTEM in der aktiven Session,
// FOLGT dem aktiven Eingabe-Desktop (Anmeldebildschirm/Winlogon, Sperre, UAC-Sicherer-
// Desktop, normaler Desktop) und liefert Frames über stdin/stdout an den Dienst.
// Protokoll: 8-Byte-Header (w,h LE), dann je Kommando (capture/pointer/key).
func RunCaptureHelper(monitor int) {
	// Desktop-Zuordnung gilt pro Thread → an einen OS-Thread binden.
	runtime.LockOSThread()
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	var df desktopFollower
	defer df.close()
	var src screenSource
	defer func() {
		if src != nil {
			src.Close()
		}
	}()
	// ensure erzeugt die Quelle (neu) auf dem aktuellen Eingabe-Desktop.
	ensure := func() {
		if _, changed := df.follow(); changed || src == nil {
			if src != nil {
				src.Close()
				src = nil
			}
			if s, e := newCaptureSource(discard, monitor); e == nil {
				src = s
			}
		}
	}
	ensure()
	if src == nil {
		os.Exit(2)
	}
	w, h := src.Bounds()
	out := make([]byte, w*h*4) // feste Ausgabegröße (Framebuffer ist fix; schwarz = Platzhalter)

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
			ensure() // Desktop-Wechsel (Login/Sperre/UAC) verfolgen
			px := out
			if src != nil {
				p, e := src.Capture()
				switch {
				case e != nil:
					src.Close()
					src = nil
				case len(p) == len(out):
					px = p
				default:
					// Auflösung hat sich geändert (Frame-Größe weicht ab) -> Helfer
					// beenden. Der Server startet einen neuen Helfer mit neuer Größe;
					// der RFB-Server meldet sie dann via DesktopSize an den Client.
					return
				}
			}
			if _, err := os.Stdout.Write(px); err != nil {
				return
			}
		case cmdPointer: // mask(1) + x(2 LE) + y(2 LE)
			if _, err := io.ReadFull(os.Stdin, arg); err != nil {
				return
			}
			if in, ok := src.(inputSink); ok {
				in.Pointer(int(arg[0]), int(binary.LittleEndian.Uint16(arg[1:])), int(binary.LittleEndian.Uint16(arg[3:])))
			}
		case cmdKey: // down(1) + keysym(4 LE)
			if _, err := io.ReadFull(os.Stdin, arg); err != nil {
				return
			}
			if in, ok := src.(inputSink); ok {
				in.Key(arg[0] != 0, binary.LittleEndian.Uint32(arg[1:]))
			}
		case cmdSetClipboard: // len(4 LE) + Text (UTF-8)
			l := make([]byte, 4)
			if _, err := io.ReadFull(os.Stdin, l); err != nil {
				return
			}
			text := make([]byte, binary.LittleEndian.Uint32(l))
			if _, err := io.ReadFull(os.Stdin, text); err != nil {
				return
			}
			if cs, ok := src.(clipboardSource); ok {
				cs.SetClipboard(string(text))
			}
		case cmdGetClipboard: // Antwort: changed(1) [+ len(4 LE) + Text]
			var text string
			var changed bool
			if cs, ok := src.(clipboardSource); ok {
				text, changed = cs.GetClipboard()
			}
			if !changed {
				if _, err := os.Stdout.Write([]byte{0}); err != nil {
					return
				}
			} else {
				resp := []byte{1, 0, 0, 0, 0}
				binary.LittleEndian.PutUint32(resp[1:], uint32(len(text)))
				if _, err := os.Stdout.Write(append(resp, text...)); err != nil {
					return
				}
			}
		default:
			return
		}
	}
}

const (
	cmdCapture      = 1
	cmdPointer      = 2
	cmdKey          = 3
	cmdGetClipboard = 4
	cmdSetClipboard = 5
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

func startCaptureHelper(log *slog.Logger, monitor int) (screenSource, error) {
	sess := windows.WTSGetActiveConsoleSessionId()
	if sess == 0xFFFFFFFF {
		return nil, fmt.Errorf("keine aktive konsolen-sitzung")
	}
	// SYSTEM-Token in der aktiven Session: deckt auch Anmeldebildschirm, Sperre und
	// UAC (sicherer Desktop) ab. Fallback: Token des angemeldeten Nutzers.
	token, err := systemSessionToken(sess)
	if err != nil {
		if e2 := windows.WTSQueryUserToken(sess, &token); e2 != nil {
			return nil, fmt.Errorf("kein session-token (SYSTEM: %v): %w", err, e2)
		}
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

	cmdline, _ := windows.UTF16PtrFromString(fmt.Sprintf(`"%s" __capture %d`, exe, monitor))
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

func (h *helperSource) SetClipboard(text string) {
	b := []byte{cmdSetClipboard, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:], uint32(len(text)))
	_, _ = h.stdinW.Write(append(b, text...))
}

func (h *helperSource) GetClipboard() (string, bool) {
	if _, err := h.stdinW.Write([]byte{cmdGetClipboard}); err != nil {
		return "", false
	}
	flag := make([]byte, 1)
	if _, err := io.ReadFull(h.stdoutR, flag); err != nil || flag[0] == 0 {
		return "", false
	}
	l := make([]byte, 4)
	if _, err := io.ReadFull(h.stdoutR, l); err != nil {
		return "", false
	}
	text := make([]byte, binary.LittleEndian.Uint32(l))
	if _, err := io.ReadFull(h.stdoutR, text); err != nil {
		return "", false
	}
	return string(text), true
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
