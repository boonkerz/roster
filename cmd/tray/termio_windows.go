package main

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// makeRaw schaltet die Windows-Konsole auf VT-Durchreichung: Eingaben ungefiltert
// (keine Zeilenpufferung, kein Echo) als VT-Sequenzen, Ausgaben mit VT-Verarbeitung.
func makeRaw() (func(), error) {
	in := windows.Handle(os.Stdin.Fd())
	out := windows.Handle(os.Stdout.Fd())
	var oldIn, oldOut uint32
	if err := windows.GetConsoleMode(in, &oldIn); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(out, &oldOut); err != nil {
		return nil, err
	}
	newIn := oldIn &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT)
	newIn |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(in, newIn); err != nil {
		return nil, err
	}
	newOut := oldOut | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.ENABLE_PROCESSED_OUTPUT
	if err := windows.SetConsoleMode(out, newOut); err != nil {
		_ = windows.SetConsoleMode(in, oldIn)
		return nil, err
	}
	return func() {
		_ = windows.SetConsoleMode(in, oldIn)
		_ = windows.SetConsoleMode(out, oldOut)
	}, nil
}

func termSize() (cols, rows int, err error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info); err != nil {
		return 0, 0, err
	}
	return int(info.Window.Right - info.Window.Left + 1), int(info.Window.Bottom - info.Window.Top + 1), nil
}

// watchResize kennt unter Windows kein SIGWINCH – daher sekündlich vergleichen.
func watchResize(ch chan<- struct{}) func() {
	done := make(chan struct{})
	go func() {
		lastC, lastR, _ := termSize()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c, r, err := termSize()
				if err == nil && (c != lastC || r != lastR) {
					lastC, lastR = c, r
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
