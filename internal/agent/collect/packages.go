package collect

import (
	"context"
	"os"
	"os/exec"
	"runtime"
)

// InstallPackage installiert ein Paket über den auf dem System verfügbaren
// Paketmanager. ids enthält die Paket-Kennung je Manager (winget/choco/apt/dnf/brew).
// Rückgabe: Exit-Code + kombinierte Ausgabe.
func InstallPackage(ctx context.Context, ids map[string]string) (int, string) {
	type mgr struct {
		key, bin string
		args     []string // "{id}" wird durch die Paket-ID ersetzt
		env      []string
	}
	var mgrs []mgr
	switch runtime.GOOS {
	case "windows":
		mgrs = []mgr{
			{"winget", "winget", []string{"install", "--id", "{id}", "-e", "--silent",
				"--accept-package-agreements", "--accept-source-agreements"}, nil},
			{"choco", "choco", []string{"install", "{id}", "-y"}, nil},
		}
	case "linux":
		mgrs = []mgr{
			{"apt", "apt-get", []string{"install", "-y", "{id}"}, []string{"DEBIAN_FRONTEND=noninteractive"}},
			{"dnf", "dnf", []string{"install", "-y", "{id}"}, nil},
		}
	case "darwin":
		mgrs = []mgr{
			{"brew", "brew", []string{"install", "{id}"}, nil},
		}
	}

	for _, m := range mgrs {
		id := ids[m.key]
		if id == "" {
			continue
		}
		bin, err := exec.LookPath(m.bin)
		if err != nil {
			continue
		}
		args := make([]string, len(m.args))
		for i, a := range m.args {
			if a == "{id}" {
				args[i] = id
			} else {
				args[i] = a
			}
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		if m.env != nil {
			cmd.Env = append(os.Environ(), m.env...)
		}
		out, err := cmd.CombinedOutput()
		exit := 0
		if err != nil {
			exit = 1
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			}
		}
		return exit, m.key + " " + id + ":\n" + string(out)
	}
	return -1, "kein passender Paketmanager oder keine Paket-ID für dieses System (" + runtime.GOOS + ")"
}
