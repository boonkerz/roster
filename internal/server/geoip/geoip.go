// Package geoip ordnet IP-Adressen ohne externen Dienst einem Land zu. Die Datenbank
// (ip-location-db „geo-whois-asn-country“, Public Domain) ist kompakt ins Binary
// eingebettet und wird beim ersten Zugriff entpackt; sie wird mit jedem Release erneuert
// (siehe gen/main.go bzw. `make geoip`).
package geoip

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
)

//go:embed data/*.gz data/version.txt
var files embed.FS

const (
	v4Rec = 6  // 4 Byte Start + 2 Byte Land
	v6Rec = 10 // 8 Byte (obere 64 Bit) + 2 Byte Land
)

var (
	once    sync.Once
	v4, v6  []byte
	version string
)

func load() {
	once.Do(func() {
		v4 = unpack("data/ipv4.bin.gz")
		v6 = unpack("data/ipv6.bin.gz")
		if b, err := files.ReadFile("data/version.txt"); err == nil {
			version = strings.TrimSpace(string(b))
		}
	})
}

func unpack(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		return nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil
	}
	return out
}

// Version ist das Stichdatum der eingebetteten Datenbank (JJJJ-MM-TT).
func Version() string {
	load()
	return version
}

// Country liefert den ISO-3166-Ländercode (zwei Großbuchstaben) einer IP oder "" für
// unbekannte, private und ungültige Adressen. Ein CIDR-Netz wird nach seiner Netzadresse
// zugeordnet.
func Country(ip string) string {
	ip = strings.TrimSpace(ip)
	if i := strings.IndexByte(ip, '/'); i > 0 {
		ip = ip[:i]
	}
	addr := net.ParseIP(ip)
	if addr == nil || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return ""
	}
	load()
	if a4 := addr.To4(); a4 != nil {
		return lookup(v4, v4Rec, 4, func(i int) int {
			return bytes.Compare(v4[i*v4Rec:i*v4Rec+4], a4)
		}, len(v4)/v4Rec)
	}
	a16 := addr.To16()
	if a16 == nil {
		return ""
	}
	hi := a16[:8]
	return lookup(v6, v6Rec, 8, func(i int) int {
		return bytes.Compare(v6[i*v6Rec:i*v6Rec+8], hi)
	}, len(v6)/v6Rec)
}

// lookup sucht den letzten Eintrag, dessen Start <= Adresse ist (cmp vergleicht Eintrag i
// mit der Adresse), und liefert dessen Ländercode.
func lookup(data []byte, rec, key int, cmp func(i int) int, n int) string {
	if n == 0 {
		return ""
	}
	// erster Index mit Start > Adresse
	idx := sort.Search(n, func(i int) bool { return cmp(i) > 0 })
	if idx == 0 {
		return ""
	}
	cc := data[(idx-1)*rec+key : (idx-1)*rec+key+2]
	if cc[0] == 0 {
		return ""
	}
	return string(cc)
}
