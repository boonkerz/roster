package netscan

import (
	"context"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// commonPorts werden zur Erreichbarkeits-/Dienst-Erkennung angetastet.
var commonPorts = []int{22, 80, 443, 445, 139, 135, 3389, 53, 8080, 5900, 21, 23, 25, 3306}

const (
	scanMaxHosts = 4096
	scanTimeout  = 400 * time.Millisecond
	scanWorkers  = 256
)

// Scan tastet die Hosts eines CIDR-Bereichs per TCP an (kein Raw-Socket nötig),
// ergänzt MAC (ARP) und Hostname (Reverse-DNS). Liefert die erreichbaren Hosts.
func Scan(ctx context.Context, cidr string) ([]shared.NetworkHost, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	var ips []string
	for ip := ipnet.IP.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		ips = append(ips, ip.String())
		if len(ips) > scanMaxHosts+2 {
			break
		}
	}
	// Netz- und Broadcast-Adresse (erste/letzte) auslassen.
	if len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}
	if len(ips) > scanMaxHosts {
		ips = ips[:scanMaxHosts]
	}

	var mu sync.Mutex
	var found []shared.NetworkHost
	jobs := make(chan string, scanWorkers)
	var wg sync.WaitGroup
	for i := 0; i < scanWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				if h, ok := probeHost(ctx, ip); ok {
					mu.Lock()
					found = append(found, h)
					mu.Unlock()
				}
			}
		}()
	}
	for _, ip := range ips {
		jobs <- ip
	}
	close(jobs)
	wg.Wait()

	// MAC-Adressen aus der ARP-Tabelle ergänzen (nach dem Antasten gefüllt).
	arp := arpTable()
	for i := range found {
		if mac := arp[found[i].IP]; mac != "" {
			found[i].MAC = mac
		}
		found[i].Hostname = reverseDNS(found[i].IP)
	}
	sort.Slice(found, func(i, j int) bool { return ipLess(found[i].IP, found[j].IP) })
	return found, nil
}

// probeHost gilt als erreichbar, wenn ein Port offen ist oder die Verbindung aktiv
// abgelehnt wird (RST = Host da). Offene Ports werden gesammelt.
func probeHost(ctx context.Context, ip string) (shared.NetworkHost, bool) {
	alive := false
	var ports []int
	d := net.Dialer{Timeout: scanTimeout}
	for _, p := range commonPorts {
		select {
		case <-ctx.Done():
			return shared.NetworkHost{}, false
		default:
		}
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(p)))
		if err == nil {
			conn.Close()
			alive = true
			ports = append(ports, p)
			continue
		}
		if strings.Contains(err.Error(), "refused") {
			alive = true
		}
	}
	if !alive {
		return shared.NetworkHost{}, false
	}
	return shared.NetworkHost{IP: ip, Ports: ports}, true
}

func reverseDNS(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

var (
	reIPv4 = regexp.MustCompile(`\b(\d{1,3}(?:\.\d{1,3}){3})\b`)
	reMAC  = regexp.MustCompile(`\b([0-9a-fA-F]{1,2}(?:[:-][0-9a-fA-F]{1,2}){5})\b`)
)

// arpTable liest die ARP-Tabelle (`arp -a`) und liefert IP->MAC (normalisiert).
func arpTable() map[string]string {
	out := map[string]string{}
	cmd := exec.Command("arp", "-a")
	data, err := cmd.Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		ip := reIPv4.FindString(line)
		mac := reMAC.FindString(line)
		if ip == "" || mac == "" {
			continue
		}
		out[ip] = normalizeMAC(mac)
	}
	return out
}

func normalizeMAC(m string) string {
	m = strings.ToLower(strings.ReplaceAll(m, "-", ":"))
	parts := strings.Split(m, ":")
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ia == nil || ib == nil {
		return a < b
	}
	for i := 0; i < 4; i++ {
		if ia[i] != ib[i] {
			return ia[i] < ib[i]
		}
	}
	return false
}
