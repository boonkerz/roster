//go:build ignore

// gen wandelt die CSV-Dateien von ip-location-db (geo-whois-asn-country, Public Domain /
// PDDL) in das kompakte Binärformat von internal/server/geoip um:
//
//	go run ./internal/server/geoip/gen -in <ordner mit *-ipv4-num.csv/*-ipv6-num.csv> -out internal/server/geoip/data
//
// IPv4: je Eintrag 4 Byte Start + 2 Byte Ländercode; IPv6: 8 Byte (obere 64 Bit des
// Starts) + 2 Byte. Das Ende eines Bereichs ist der nächste Start − 1; Lücken werden als
// Eintrag mit leerem Code ("\x00\x00") kodiert, benachbarte Bereiche gleichen Landes
// zusammengefasst.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type rec struct {
	start, end *big.Int
	cc         string
}

func readCSV(path string) ([]rec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []rec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		parts := strings.Split(strings.TrimSpace(sc.Text()), ",")
		if len(parts) != 3 {
			continue
		}
		s, ok1 := new(big.Int).SetString(parts[0], 10)
		e, ok2 := new(big.Int).SetString(parts[1], 10)
		cc := strings.ToUpper(strings.TrimSpace(parts[2]))
		if !ok1 || !ok2 || len(cc) != 2 {
			continue
		}
		out = append(out, rec{s, e, cc})
	}
	return out, sc.Err()
}

// encode schreibt Start (width Byte, big endian, bei IPv6 die oberen 64 Bit) + Code.
func encode(recs []rec, width int) []byte {
	var buf bytes.Buffer
	one := big.NewInt(1)
	shift := uint(0)
	if width == 8 {
		shift = 64
	}
	key := func(v *big.Int) []byte {
		b := make([]byte, width)
		k := new(big.Int).Rsh(v, shift)
		kb := k.Bytes()
		copy(b[width-len(kb):], kb)
		return b
	}
	var lastKey []byte
	lastCC := ""
	put := func(k []byte, cc string) {
		if cc == lastCC && lastKey != nil {
			return // benachbart, gleiches Land → zusammenfassen
		}
		if lastKey != nil && bytes.Equal(k, lastKey) {
			return // feiner als die Auflösung (IPv6 unterhalb /64) → erster gewinnt
		}
		buf.Write(k)
		if cc == "" {
			buf.Write([]byte{0, 0})
		} else {
			buf.WriteString(cc)
		}
		lastKey, lastCC = append([]byte(nil), k...), cc
	}
	var prevEnd *big.Int
	for _, r := range recs {
		if prevEnd != nil {
			gap := new(big.Int).Add(prevEnd, one)
			if gap.Cmp(r.start) < 0 {
				put(key(gap), "")
			}
		}
		put(key(r.start), r.cc)
		prevEnd = r.end
	}
	if prevEnd != nil {
		put(key(new(big.Int).Add(prevEnd, one)), "")
	}
	return buf.Bytes()
}

func writeGz(path string, data []byte) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(data)
	zw.Close()
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func main() {
	in := flag.String("in", ".", "Ordner mit den CSV-Dateien")
	out := flag.String("out", "internal/server/geoip/data", "Zielordner")
	flag.Parse()
	for _, v := range []struct {
		glob  string
		width int
		name  string
	}{{"*-ipv4-num.csv", 4, "ipv4.bin.gz"}, {"*-ipv6-num.csv", 8, "ipv6.bin.gz"}} {
		m, _ := filepath.Glob(filepath.Join(*in, v.glob))
		if len(m) == 0 {
			fmt.Fprintln(os.Stderr, "keine Datei für", v.glob)
			os.Exit(1)
		}
		recs, err := readCSV(m[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		data := encode(recs, v.width)
		if err := writeGz(filepath.Join(*out, v.name), data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s: %d Zeilen → %d Einträge, %d Byte roh\n", filepath.Base(m[0]), len(recs), len(data)/(v.width+2), len(data))
	}
	_ = os.WriteFile(filepath.Join(*out, "version.txt"), []byte(time.Now().Format("2006-01-02")+"\n"), 0o644)
}
