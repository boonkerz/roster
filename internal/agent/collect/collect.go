// Package collect sammelt das Hardware-/Netzwerk-Inventar plattformübergreifend.
// Basisdaten kommen von gopsutil + der Standardbibliothek; OS-spezifische
// Ergänzungen (z.B. Seriennummer) liefern Dateien mit Build-Tags (hostinfo_*.go).
package collect

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/boonkerz/roster/internal/shared"
)

// Collect erstellt einen vollständigen Inventar-Snapshot.
func Collect(ctx context.Context, agentVersion string) shared.Inventory {
	inv := shared.Inventory{
		OS:           runtime.GOOS,
		AgentVersion: agentVersion,
		CollectedAt:  time.Now().UTC(),
	}

	if hi, err := host.InfoWithContext(ctx); err == nil {
		inv.Hostname = hi.Hostname
		inv.OS = hi.Platform
		inv.OSVersion = hi.PlatformVersion
		inv.UptimeSec = hi.Uptime
	}
	if inv.Hostname == "" {
		inv.Hostname, _ = net.LookupCNAME("") // Fallback; meist liefert host.Info bereits den Namen
	}

	if cpus, err := cpu.InfoWithContext(ctx); err == nil && len(cpus) > 0 {
		inv.CPUModel = strings.TrimSpace(cpus[0].ModelName)
		sockets := map[string]bool{}
		for _, c := range cpus {
			sockets[c.PhysicalID] = true
		}
		inv.CPUSockets = len(sockets)
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		inv.CPUCores = n // physische Kerne
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		inv.CPUThreads = n // logische Prozessoren (Threads)
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		inv.MemoryBytes = vm.Total
	}

	inv.Disks = collectDisks(ctx)
	inv.Interfaces = collectInterfaces()

	// OS-spezifische Hardware-Identität (Hersteller/Modell/Seriennummer).
	vendor, model, serial := hardwareIdentity(ctx)
	inv.Vendor, inv.Model, inv.Serial = vendor, model, serial

	// OS-spezifisch: installierte Software, Drucker, angemeldete Benutzer.
	inv.Software, inv.Printers, inv.LoggedInUsers = osExtras(ctx)

	// OS-spezifisch: GPUs und physische Festplatten.
	inv.GPUs, inv.PhysicalDisks = hwExtras(ctx)

	// Lauschende Sockets (Angriffsfläche / „nach außen offen").
	inv.ListenPorts = ListenPorts(ctx)

	// Docker (nur wo die docker-CLI vorhanden ist).
	inv.Containers = DockerContainers(ctx)
	inv.Images = DockerImages(ctx)

	// Temperatursensoren (leer auf VMs / Systemen ohne auslesbare Sensoren).
	inv.Temperatures = Temperatures(ctx)
	inv.Fans = Fans()

	// Proxmox VE (nur auf PVE-Hosts): Gäste + Backup-Status.
	inv.Proxmox = Proxmox(ctx)

	return inv
}

// OSUpdates ermittelt verfügbare Betriebssystem-Updates. Der Aufruf kann teuer sein
// (Paketmanager/Windows Update) und sollte selten/asynchron erfolgen. Gibt nil zurück,
// wenn kein unterstützter Mechanismus gefunden wurde (Status "unbekannt").
func OSUpdates(ctx context.Context) *shared.OSUpdateInfo {
	items, ok := osUpdateList(ctx)
	if !ok {
		return nil
	}
	const maxItems = 500
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return &shared.OSUpdateInfo{Count: len(items), CheckedAt: time.Now().UTC(), Items: items}
}

// realFSTypes sind „echte" Dateisysteme, die als Datenträger gezeigt werden – inkl.
// overlay (Container-Root) und der Windows/macOS-Typen.
var realFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "btrfs": true, "xfs": true,
	"vfat": true, "fat32": true, "msdos": true, "exfat": true,
	"ntfs": true, "ntfs3": true, "fuseblk": true,
	"zfs": true, "f2fs": true, "jfs": true, "reiserfs": true, "ufs": true, "bcachefs": true,
	"apfs": true, "hfs": true, "hfsplus": true,
	"overlay": true,
}

// capOutput kappt eine (potenziell sehr lange) Befehlsausgabe.
func capOutput(s string) string {
	const max = 8000
	if len(s) > max {
		return s[len(s)-max:] // das Ende ist meist relevanter (Zusammenfassung)
	}
	return s
}

// packagesToItems wandelt Paketnamen in UpdateItems (Schweregrad "Other") um.
func packagesToItems(names []string) []shared.UpdateItem {
	out := make([]shared.UpdateItem, 0, len(names))
	for _, n := range names {
		out = append(out, shared.UpdateItem{Name: n, Severity: "Other"})
	}
	return out
}

func collectDisks(ctx context.Context) []shared.Disk {
	// Alle Mounts holen und selbst auf echte Dateisysteme filtern – sonst fehlen z.B.
	// Container-Roots (overlay), die der "physical only"-Filter ausschließt.
	parts, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []shared.Disk
	for _, p := range parts {
		if !realFSTypes[strings.ToLower(p.Fstype)] || seen[p.Device] {
			continue
		}
		// Datei-Bind-Mounts (z.B. /etc/resolv.conf in Containern) überspringen.
		if fi, err := os.Stat(p.Mountpoint); err != nil || !fi.IsDir() {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		seen[p.Device] = true
		out = append(out, shared.Disk{
			Name: p.Mountpoint, FSType: p.Fstype,
			SizeBytes: u.Total, FreeBytes: u.Free, UsedPercent: u.UsedPercent,
		})
	}
	return out
}

// collectInterfaces liefert alle aktiven Schnittstellen mit MAC und IP-Adressen.
func collectInterfaces() []shared.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []shared.Interface
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.HardwareAddr == nil {
			continue
		}
		// Container-seitige virtuelle Schnittstellen (veth*) sind für die
		// Asset-Inventarisierung irrelevant und nur Rauschen.
		if strings.HasPrefix(ifc.Name, "veth") {
			continue
		}
		entry := shared.Interface{Name: ifc.Name, MAC: ifc.HardwareAddr.String()}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.IsLinkLocalUnicast() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				entry.IPv4 = append(entry.IPv4, v4.String())
			} else {
				entry.IPv6 = append(entry.IPv6, ip.String())
			}
		}
		if entry.MAC == "" && len(entry.IPv4) == 0 && len(entry.IPv6) == 0 {
			continue
		}
		out = append(out, entry)
	}
	return out
}
