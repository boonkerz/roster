package main

import "golang.org/x/sys/unix"

// Linux nutzt TCGETS/TCSETS; BSD/macOS TIOCGETA/TIOCSETA.
const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)
