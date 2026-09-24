// Package clientinstall ist der gemeinsame Unterbau für `--install`/`--uninstall`
// der nativen Client-Programme (roster-viewer, roster-tray): kopiert das laufende
// Binary samt mitgelieferter SDL3-Laufzeit in ein Programmverzeichnis und legt
// Starter an (Linux: .desktop-Einträge, Windows: Verknüpfungen). Kein root nötig –
// alles landet im Nutzerprofil, sofern kein anderes Präfix gewählt wird.
package clientinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// runtimeLibs sind die Laufzeitbibliotheken, die neben dem Binary liegen dürfen
// (Downloads für Windows/macOS bringen sie im ZIP mit) und mitkopiert werden.
func runtimeLibs() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"SDL3.dll"}
	case "darwin":
		return []string{"libSDL3.dylib"}
	}
	return nil
}

// Self liefert den aufgelösten Pfad des laufenden Programms.
func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return exe, nil
}

// InstallExecutable kopiert das laufende Programm nach dir/name (plus SDL3-Laufzeit,
// falls sie neben der Quelle liegt) und liefert den Zielpfad. Läuft das Programm
// bereits von dort, wird nichts kopiert. Ein vorhandenes Ziel wird ersetzt – unter
// Windows über Umbenennen, damit auch eine gerade laufende Kopie ersetzt werden kann.
func InstallExecutable(dir, name string) (string, error) {
	src, err := Self()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	if same, _ := sameFile(src, dst); same {
		return dst, nil
	}
	if err := copyFile(src, dst, 0o755); err != nil {
		return "", fmt.Errorf("%s: %w", dst, err)
	}
	srcDir := filepath.Dir(src)
	for _, lib := range runtimeLibs() {
		from := filepath.Join(srcDir, lib)
		if _, err := os.Stat(from); err != nil {
			continue // nicht mitgeliefert – z. B. Linux, SDL3 aus der Distribution
		}
		to := filepath.Join(dir, lib)
		if same, _ := sameFile(from, to); same {
			continue
		}
		if err := copyFile(from, to, 0o644); err != nil {
			return "", fmt.Errorf("%s: %w", to, err)
		}
	}
	return dst, nil
}

// RemoveExecutable entfernt dir/name. Liegen keine weiteren Roster-Programme mehr im
// Verzeichnis, gehen die mitkopierten Laufzeitbibliotheken (und ein leeres
// Verzeichnis) gleich mit. Unter Windows kann das laufende Programm sich nicht
// selbst löschen; es wird dann in name.old umbenannt und beim nächsten --install
// aufgeräumt.
func RemoveExecutable(dir, name string) error {
	dst := filepath.Join(dir, name)
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		if runtime.GOOS != "windows" {
			return err
		}
		if err := os.Rename(dst, dst+".old"); err != nil {
			return err
		}
	}
	_ = os.Remove(dst + ".old")
	if others, _ := filepath.Glob(filepath.Join(dir, "roster-*")); len(others) > 0 {
		return nil
	}
	for _, lib := range runtimeLibs() {
		_ = os.Remove(filepath.Join(dir, lib))
	}
	_ = os.Remove(dir) // nur, wenn leer
	return nil
}

// PathHint liefert einen Hinweis, wenn dir nicht im PATH liegt (sonst leer).
func PathHint(dir string) string {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		if a, err := filepath.Abs(p); err == nil && a == dir {
			return ""
		}
	}
	return fmt.Sprintf("Hinweis: %s liegt nicht im PATH – zum Start per Kommandozeile den vollen Pfad nutzen oder PATH ergänzen.", dir)
}

// WriteFile schreibt eine Datei und legt das Verzeichnis bei Bedarf an.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// RemoveIfExists löscht eine Datei; eine fehlende Datei ist kein Fehler.
func RemoveIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// HomeDir liefert das Nutzerverzeichnis (Fallback: $HOME).
func HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}

// copyFile schreibt erst eine temporäre Datei und benennt dann um, damit nie ein
// halb geschriebenes Binary am Zielpfad liegt. Unter Windows wird ein laufendes
// Ziel vorher nach .old geschoben (Umbenennen ist dort erlaubt, Überschreiben nicht).
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	if runtime.GOOS == "windows" {
		old := dst + ".old"
		_ = os.Remove(old)
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, old); err != nil {
				os.Remove(tmpName)
				return err
			}
		}
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(dst + ".old") // schlägt fehl, solange die alte Kopie läuft – egal
	}
	return nil
}

// ExeName hängt unter Windows ".exe" an.
func ExeName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		return name + ".exe"
	}
	return name
}
