package main

import (
	"fmt"
	"net"
	"net/url"
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
	return spawnSFTP(target)
}

// expandSFTPCommand füllt die Platzhalter des konfigurierten Kommandos.
func expandSFTPCommand(tmpl string, cfg *config, d device, target string) []string {
	rep := strings.NewReplacer(
		"{{host}}", deviceAddress(d),
		"{{user}}", strings.TrimSpace(cfg.SFTPUser),
		"{{url}}", target,
		"{{name}}", d.Hostname,
	)
	fields := strings.Fields(tmpl)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, rep.Replace(f))
	}
	return out
}
