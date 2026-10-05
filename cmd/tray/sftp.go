package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Der SFTP-Knopf arbeitet – anders als Terminal und Viewer – NICHT über den
// Roster-Tunnel, sondern baut eine direkte SSH-Verbindung zum Gerät auf: er
// übergibt dem Desktop eine sftp://-Adresse (Dateimanager wie Nautilus/Dolphin)
// oder startet ein konfiguriertes Programm (FileZilla, WinSCP …). Voraussetzung
// sind also ein SSH-Server auf dem Gerät und Netzwerksicht vom Arbeitsplatz aus.

// virtualIfacePrefixes sind Schnittstellen von Containern/VMs/Bridges – deren
// Adressen führen nicht zum Gerät selbst.
var virtualIfacePrefixes = []string{"docker", "br-", "virbr", "veth", "vmnet", "vboxnet", "lo"}

// deviceAddress wählt die Adresse für die SSH-Verbindung: erste brauchbare IPv4
// einer echten Schnittstelle, sonst der Hostname, sonst die öffentliche IP.
func deviceAddress(d device) string {
	for _, i := range d.Interfaces {
		if isVirtualIface(i.Name) {
			continue
		}
		if ip := usableIPv4(i.IPv4); ip != "" {
			return ip
		}
	}
	if h := strings.TrimSpace(d.Hostname); h != "" {
		return h // per DNS/NetBIOS auflösbar, wenn keine IP im Inventar steht
	}
	return strings.TrimSpace(d.PublicIP)
}

func isVirtualIface(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// usableIPv4 filtert Loopback, Link-Local und Unsinn heraus.
func usableIPv4(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// sftpURL baut die sftp://-Adresse für ein Gerät.
func sftpURL(cfg *config, d device) (string, error) {
	host := deviceAddress(d)
	if host == "" {
		return "", fmt.Errorf("keine Adresse bekannt (keine IP im Inventar)")
	}
	u := &url.URL{Scheme: "sftp", Host: host, Path: "/"}
	if user := strings.TrimSpace(cfg.SFTPUser); user != "" {
		u.User = url.User(user)
	}
	return u.String(), nil
}

// openSFTP startet das Dateiübertragungs-Programm für ein Gerät und meldet zurück,
// welches es war (für die Statuszeile).
func openSFTP(cfg *config, d device) (string, error) {
	target, err := sftpURL(cfg, d)
	if err != nil {
		return "", err
	}
	if tmpl := strings.TrimSpace(cfg.SFTPCommand); tmpl != "" {
		args := expandSFTPCommand(tmpl, cfg, d, target)
		if len(args) == 0 {
			return "", fmt.Errorf("sftp_command ist leer")
		}
		return args[0], spawn(args[0], args[1:]...)
	}
	if key := strings.TrimSpace(cfg.SFTPKey); key != "" {
		return spawnSFTPWithKey(cfg, d, target, key)
	}
	return spawnSFTP(target)
}

// spawnSFTPWithKey öffnet die Verbindung mit einem bestimmten Schlüssel. Eine
// sftp://-Adresse transportiert keinen Schlüssel, deshalb: WinSCP (kennt
// /privatekey=) oder sonst der sftp-Client von OpenSSH im System-Terminal.
func spawnSFTPWithKey(cfg *config, d device, target, key string) (string, error) {
	if _, err := os.Stat(key); err != nil {
		return "", fmt.Errorf("SSH-Schlüssel %s: %v", key, err)
	}
	if runtime.GOOS == "windows" {
		for _, cand := range []string{"WinSCP.exe", `C:\Program Files (x86)\WinSCP\WinSCP.exe`, `C:\Program Files\WinSCP\WinSCP.exe`} {
			if p, err := exec.LookPath(cand); err == nil {
				return "WinSCP", spawn(p, target, "/privatekey="+key)
			}
		}
	}
	userHost := deviceAddress(d)
	if user := strings.TrimSpace(cfg.SFTPUser); user != "" {
		userHost = user + "@" + userHost
	}
	title := "SFTP – " + d.Hostname
	return "sftp (" + filepath.Base(key) + ")", spawnTerminal(cfg, title, "sftp", []string{"-i", key, userHost})
}

// sshKeys listet die privaten Schlüssel in ~/.ssh: alles, wozu ein .pub existiert
// oder was id_… heißt – ohne known_hosts, config & Co.
func sshKeys() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".ssh")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, ".pub") || strings.HasPrefix(n, "known_hosts") ||
			n == "config" || n == "authorized_keys" || n == "environment" || n == "rc" {
			continue
		}
		pub := filepath.Join(dir, n+".pub")
		if _, err := os.Stat(pub); err == nil || strings.HasPrefix(n, "id_") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// expandSFTPCommand füllt die Platzhalter des konfigurierten Kommandos.
func expandSFTPCommand(tmpl string, cfg *config, d device, target string) []string {
	rep := strings.NewReplacer(
		"{{host}}", deviceAddress(d),
		"{{user}}", strings.TrimSpace(cfg.SFTPUser),
		"{{url}}", target,
		"{{name}}", d.Hostname,
		"{{key}}", strings.TrimSpace(cfg.SFTPKey),
	)
	fields := strings.Fields(tmpl)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, rep.Replace(f))
	}
	return out
}
