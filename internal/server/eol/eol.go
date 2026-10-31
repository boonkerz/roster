// Package eol ermittelt das Support-Ende (End of Life) von Betriebssystemen über die
// öffentliche endoflife.date-API (v1, kein Schlüssel). Reines Paket: HTTP-Client wird
// injiziert, die Zuordnung Gerät → Produkt/Zyklus ist eine getestete Funktion.
package eol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BaseURL der endoflife.date-API (v1); in Tests überschreibbar.
var BaseURL = "https://endoflife.date/api/v1/products/"

// Cycle ist ein Release-Zyklus eines Produkts.
type Cycle struct {
	Name        string     `json:"name"`  // z. B. "24.04", "12", "11-23h2-w"
	Label       string     `json:"label"` // z. B. "24.04 'Noble Numbat' (LTS)"
	Codename    string     `json:"codename,omitempty"`
	ReleaseDate *time.Time `json:"release_date,omitempty"`
	EOL         *time.Time `json:"eol,omitempty"`     // Ende der Sicherheitsupdates (eolFrom)
	Support     *time.Time `json:"support,omitempty"` // Ende des aktiven Supports (eoasFrom)
	IsEOL       bool       `json:"is_eol"`
	LTS         bool       `json:"lts"`
	Latest      string     `json:"latest,omitempty"`
}

type apiResponse struct {
	Result struct {
		Releases []struct {
			Name        string `json:"name"`
			Codename    string `json:"codename"`
			Label       string `json:"label"`
			ReleaseDate string `json:"releaseDate"`
			IsLTS       bool   `json:"isLts"`
			IsEOL       bool   `json:"isEol"`
			EOLFrom     string `json:"eolFrom"`
			EOASFrom    string `json:"eoasFrom"`
			Latest      *struct {
				Name string `json:"name"`
			} `json:"latest"`
		} `json:"releases"`
	} `json:"result"`
}

// Fetch lädt die Zyklen eines Produkts (z. B. "ubuntu") von endoflife.date.
func Fetch(ctx context.Context, client *http.Client, product string) ([]Cycle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+product, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "roster-rmm (eol check)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endoflife.date %s: HTTP %d", product, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return Parse(body)
}

// Parse wandelt die v1-Antwort in Zyklen um.
func Parse(body []byte) ([]Cycle, error) {
	var r apiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if len(r.Result.Releases) == 0 {
		return nil, fmt.Errorf("endoflife.date: keine Releases in der Antwort")
	}
	out := make([]Cycle, 0, len(r.Result.Releases))
	for _, rel := range r.Result.Releases {
		c := Cycle{Name: rel.Name, Label: rel.Label, Codename: rel.Codename, IsEOL: rel.IsEOL, LTS: rel.IsLTS,
			ReleaseDate: parseDate(rel.ReleaseDate), EOL: parseDate(rel.EOLFrom), Support: parseDate(rel.EOASFrom)}
		if rel.Latest != nil {
			c.Latest = rel.Latest.Name
		}
		out = append(out, c)
	}
	return out, nil
}

func parseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// Target ist ein zu prüfendes Produkt eines Geräts mit Kandidaten-Zyklen (erster Treffer gewinnt).
type Target struct {
	Kind       string // os | proxmox
	Product    string // endoflife.date-Slug
	Candidates []string
}

// Device sind die für die Zuordnung relevanten Gerätefelder.
type Device struct {
	OS, OSVersion, OSBuild, OSCodename, OSEdition, ProxmoxVersion string
}

var numRe = regexp.MustCompile(`\d+(?:\.\d+)*`)

func firstNumber(s string) string { return numRe.FindString(s) }

func major(v string) string {
	v = firstNumber(v)
	if i := strings.Index(v, "."); i > 0 {
		return v[:i]
	}
	return v
}

func majorMinor(v string) string {
	parts := strings.SplitN(firstNumber(v), ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return parts[0]
}

// Resolve ordnet ein Gerät den endoflife.date-Produkten zu. Rolling-Release-Systeme
// (Arch, CachyOS, Manjaro, Gentoo, NixOS …) liefern kein Ziel.
func Resolve(d Device) []Target {
	var out []Target
	os := strings.ToLower(strings.TrimSpace(d.OS))
	ver := strings.TrimSpace(d.OSVersion)
	add := func(product string, cands ...string) {
		var clean []string
		for _, c := range cands {
			if c != "" {
				clean = append(clean, c)
			}
		}
		if len(clean) > 0 {
			out = append(out, Target{Kind: "os", Product: product, Candidates: clean})
		}
	}
	switch {
	case strings.Contains(os, "windows"):
		if t, ok := windowsTarget(d); ok {
			out = append(out, t)
		}
	case os == "darwin" || strings.Contains(os, "macos") || strings.Contains(os, "mac os"):
		add("macos", major(ver))
	case strings.HasPrefix(os, "ubuntu"), os == "pop", os == "neon", os == "zorin":
		add("ubuntu", majorMinor(ver))
	case os == "debian" || os == "raspbian":
		add("debian", major(ver))
	case os == "fedora":
		add("fedora", major(ver))
	case os == "rhel" || os == "redhat" || strings.Contains(os, "red hat"):
		add("rhel", major(ver))
	case os == "rocky" || strings.Contains(os, "rocky"):
		add("rocky-linux", major(ver))
	case os == "almalinux" || strings.Contains(os, "alma"):
		add("almalinux", major(ver))
	case os == "centos":
		add("centos-stream", major(ver))
	case os == "alpine":
		add("alpine-linux", majorMinor(ver))
	case strings.HasPrefix(os, "opensuse-leap") || os == "opensuse":
		add("opensuse", majorMinor(ver))
	case os == "sles" || os == "sled":
		mm := majorMinor(ver)
		alt := ""
		if p := strings.SplitN(mm, ".", 2); len(p) == 2 {
			alt = p[0] + "-sp" + p[1]
		}
		add("sles", mm, alt)
	case os == "amzn" || strings.Contains(os, "amazon"):
		add("amazon-linux", major(ver))
	case os == "ol" || strings.Contains(os, "oracle"):
		add("oracle-linux", major(ver))
	case os == "freebsd":
		add("freebsd", major(ver))
	case os == "linuxmint":
		add("linuxmint", majorMinor(ver), major(ver))
	}
	if pv := major(d.ProxmoxVersion); pv != "" {
		out = append(out, Target{Kind: "proxmox", Product: "proxmox-ve", Candidates: []string{pv}})
	}
	return out
}

// windowsTarget bildet Windows-Client/-Server ab: Client-Zyklen heißen
// "11-23h2-w" (Home/Pro) bzw. "11-23h2-e" (Enterprise/Education), LTSC "…-e-lts",
// Server "2022". Die Hauptversion kommt aus der Buildnummer (≥ 22000 = Windows 11).
func windowsTarget(d Device) (Target, bool) {
	osl := strings.ToLower(d.OS)
	edition := strings.ToLower(d.OSEdition)
	if strings.Contains(osl, "server") || strings.HasPrefix(edition, "server") {
		m := regexp.MustCompile(`20\d\d`).FindString(d.OS)
		if m == "" {
			return Target{}, false
		}
		return Target{Kind: "os", Product: "windows-server", Candidates: []string{m}}, true
	}
	build := windowsBuild(d.OSBuild, d.OSVersion)
	dv := strings.ToLower(strings.TrimSpace(d.OSCodename))
	if dv == "" {
		dv = displayVersionFromBuild(build)
	}
	if dv == "" {
		return Target{}, false
	}
	maj := "10"
	if build >= 22000 || strings.Contains(osl, "windows 11") {
		maj = "11"
	}
	base := maj + "-" + dv
	var cands []string
	enterprise := strings.Contains(edition, "enterprise") || strings.Contains(edition, "education")
	switch {
	case strings.Contains(edition, "iotenterprises"), strings.Contains(edition, "iot enterprise ltsc"):
		cands = []string{base + "-iot-lts", base + "-e-lts", base + "-e"}
	case strings.HasSuffix(edition, "enterprises"), strings.Contains(edition, "ltsc"):
		cands = []string{base + "-e-lts", base + "-e"}
	case enterprise:
		cands = []string{base + "-e", base}
	default:
		cands = []string{base + "-w", base}
	}
	return Target{Kind: "os", Product: "windows", Candidates: cands}, true
}

// windowsBuild liest die Buildnummer aus "23H2 (22631.4169)" bzw. "10.0.22631 Build 22631".
func windowsBuild(osBuild, osVersion string) int {
	for _, s := range []string{osBuild, osVersion} {
		for _, m := range regexp.MustCompile(`\b(\d{4,5})(?:\.\d+)?\b`).FindAllStringSubmatch(s, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 7600 {
				return n
			}
		}
	}
	return 0
}

// displayVersionFromBuild kennt die Builds ohne DisplayVersion in der Registry (ältere Agents).
func displayVersionFromBuild(build int) string {
	switch build {
	case 26100:
		return "24h2"
	case 22631:
		return "23h2"
	case 22621:
		return "22h2"
	case 22000:
		return "21h2"
	case 19045:
		return "22h2"
	case 19044:
		return "21h2"
	case 19043:
		return "21h1"
	case 19042:
		return "20h2"
	case 19041:
		return "2004"
	case 17763:
		return "1809"
	}
	return ""
}

// Match sucht den ersten Kandidaten in den Zyklen.
func Match(cycles []Cycle, candidates []string) (Cycle, bool) {
	for _, cand := range candidates {
		for _, c := range cycles {
			if strings.EqualFold(c.Name, cand) {
				return c, true
			}
		}
	}
	return Cycle{}, false
}

// Status-Werte eines Geräts.
const (
	StatusOK      = "ok"
	StatusSoon    = "soon"
	StatusEOL     = "eol"
	StatusUnknown = "unknown"
)

// Classify bewertet einen Zyklus: eol (Support-Ende erreicht), soon (innerhalb warnDays),
// ok, unknown (kein Datum bekannt und nicht als EOL markiert).
func Classify(c Cycle, now time.Time, warnDays int) string {
	if c.EOL == nil {
		if c.IsEOL {
			return StatusEOL
		}
		return StatusUnknown
	}
	if !now.Before(*c.EOL) {
		return StatusEOL
	}
	if warnDays > 0 && now.Add(time.Duration(warnDays)*24*time.Hour).After(*c.EOL) {
		return StatusSoon
	}
	return StatusOK
}
