package remote

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"log/slog"

	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
)

// vncCacheDir ist der Ort, an dem gebündelte VNC-Server entpackt werden. Unter
// Windows ProgramData (auch aus der Nutzer-Session lesbar), sonst der User-Cache.
func vncCacheDir() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "PC-Inventory", "vnc")
		}
	}
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "pc-inventory", "vnc")
}

// ensureVNCServer stellt sicher, dass das VNC-Server-Binary (exeName) lokal
// verfügbar ist: liegt es im Cache, wird der Pfad zurückgegeben; sonst wird das
// Bundle der Plattform vom Server geladen und entpackt (on-demand, einmalig).
func ensureVNCServer(ctx context.Context, client *transport.Client, agentToken, platform, exeName string, log *slog.Logger) (string, error) {
	dir := vncCacheDir()
	exePath := filepath.Join(dir, exeName)
	if st, err := os.Stat(exePath); err == nil && !st.IsDir() {
		return exePath, nil
	}
	log.Info("vnc-bundle wird geladen", "platform", platform)
	data, _, err := client.DownloadVNCBundle(ctx, agentToken, platform)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := unzipTo(data, dir); err != nil {
		return "", fmt.Errorf("vnc-bundle entpacken: %w", err)
	}
	if st, err := os.Stat(exePath); err != nil || st.IsDir() {
		return "", fmt.Errorf("%s nicht im vnc-bundle enthalten", exeName)
	}
	log.Info("vnc-bundle entpackt", "dir", dir)
	return exePath, nil
}

// unzipTo entpackt ein ZIP (im Speicher) nach dir – mit Zip-Slip-Schutz.
func unzipTo(data []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	root := filepath.Clean(dir)
	for _, f := range zr.File {
		target := filepath.Join(dir, f.Name)
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			continue // Zip-Slip
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}
