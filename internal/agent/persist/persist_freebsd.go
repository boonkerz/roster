//go:build freebsd

package persist

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Ensure richtet die upgrade-feste Installation ein (idempotent; nur als root unter dem
// Standardpfad). Läuft beim Agent-Start – also auch nach jedem Selbst-Update.
func Ensure(ctx context.Context, log *slog.Logger, version string) {
	if os.Geteuid() != 0 {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if exe != BinPath {
		log.Info("persist: agent läuft nicht aus "+BinPath+" – keine Paketregistrierung", "exe", exe)
		return
	}
	writeIfChanged(log, RCPath, RCScript, 0o755)
	_ = os.MkdirAll(filepath.Dir(RCConfPath), 0o755)
	writeIfChanged(log, RCConfPath, RCConf, 0o644)

	if _, err := os.Stat("/usr/local/opnsense/version"); err != nil {
		return // kein OPNsense: rc.d + rc.conf.d reichen
	}
	bin, err := os.ReadFile(BinPath)
	if err != nil {
		return
	}
	ver := pkgVersion(version, sum(bin))
	marker := filepath.Join(RepoDir, ".registered-"+ver+"-"+sum(bin)[:12])
	if _, err := os.Stat(marker); err == nil {
		return
	}
	writeIfChanged(log, VersionPath, VersionJSON(ver), 0o644)
	if err := buildAndRegister(ctx, ver); err != nil {
		log.Warn("persist: OPNsense-Plugin-Registrierung fehlgeschlagen", "err", err)
		return
	}
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
	log.Info("persist: als OPNsense-Plugin registriert", "paket", PkgName, "version", ver)
}

func buildAndRegister(ctx context.Context, ver string) error {
	work := filepath.Join(RepoDir, ".work")
	all := filepath.Join(RepoDir, "All")
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(all, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "plist"), []byte(Plist), 0o644); err != nil {
		return err
	}
	// Erst mit Wildcard-ABI (übersteht FreeBSD-Major-Wechsel), sonst mit der Host-ABI.
	var pkgFile string
	var lastErr error
	for _, abi := range []string{"FreeBSD:*:amd64", hostABI(ctx)} {
		if abi == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(work, "manifest"), []byte(Manifest(ver, abi)), 0o644); err != nil {
			return err
		}
		out := filepath.Join(work, "out")
		_ = os.RemoveAll(out)
		_ = os.MkdirAll(out, 0o755)
		if _, err := run(ctx, "pkg", "create", "-M", filepath.Join(work, "manifest"), "-p", filepath.Join(work, "plist"), "-r", "/", "-o", out); err != nil {
			lastErr = err
			continue
		}
		files, _ := filepath.Glob(filepath.Join(out, PkgName+"-*.pkg"))
		if len(files) == 0 {
			lastErr = errString("pkg create lieferte keine Datei")
			continue
		}
		// pkg add -f: registriert die vorhandenen Dateien als Paket (Inhalt identisch;
		// pkg ersetzt per Umbenennen, das laufende Binary bleibt unberührt).
		if _, err := run(ctx, "pkg", "add", "-f", files[0]); err != nil {
			lastErr = err
			continue
		}
		pkgFile = files[0]
		break
	}
	if pkgFile == "" {
		return lastErr
	}
	// Alte Versionen raus, neue ins Repo, Katalog neu erzeugen.
	old, _ := filepath.Glob(filepath.Join(all, PkgName+"-*.pkg"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	if err := os.Rename(pkgFile, filepath.Join(all, filepath.Base(pkgFile))); err != nil {
		return err
	}
	if _, err := run(ctx, "pkg", "repo", RepoDir); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(RepoConf), 0o755)
	if cur, _ := os.ReadFile(RepoConf); string(cur) != RepoConfText {
		if err := os.WriteFile(RepoConf, []byte(RepoConfText), 0o644); err != nil {
			return err
		}
	}
	// In config.xml als Plugin eintragen (Firmware → Plugins kennt es dann als installiert).
	if _, err := os.Stat(RegisterPHP); err == nil {
		if _, err := run(ctx, RegisterPHP, "install", PkgName); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(work)
	return nil
}

func hostABI(ctx context.Context) string {
	out, err := run(ctx, "pkg", "config", "abi")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Env = append(os.Environ(), "ASSUME_ALWAYS_YES=yes", "IGNORE_OSVERSION=yes")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		return string(out), errString(name + " " + strings.Join(args, " ") + ": " + err.Error() + ": " + msg)
	}
	return string(out), nil
}

type errString string

func (e errString) Error() string { return string(e) }

func writeIfChanged(log *slog.Logger, path, content string, mode os.FileMode) {
	if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		log.Warn("persist: schreiben fehlgeschlagen", "datei", path, "err", err)
		return
	}
	_ = os.Chmod(path, mode)
}
