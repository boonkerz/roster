package sdlui

import (
	"fmt"
	"syscall"
)

func dlopenSDL(name string) (uintptr, error) {
	h, err := syscall.LoadLibrary(name)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return uintptr(h), nil
}

func dlsymSDL(lib uintptr, name string) (uintptr, error) {
	addr, err := syscall.GetProcAddress(syscall.Handle(lib), name)
	return addr, err
}
