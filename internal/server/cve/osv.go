// Package cve gleicht installierte Software gegen die öffentliche OSV.dev-Datenbank
// ab (kostenlos, kein Schlüssel). Abdeckung ist am stärksten für Linux-Distributions-
// pakete (dpkg/rpm); Windows-/macOS-Programme sind best effort.
package cve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

const (
	batchURL     = "https://api.osv.dev/v1/querybatch"
	vulnURL      = "https://api.osv.dev/v1/vulns/"
	maxSW        = 500  // Obergrenze abgefragter Pakete je Gerät
	maxVulns     = 1000 // Obergrenze neu von OSV geladener Detail-Datensätze je Scan
	fetchWorkers = 6    // parallele Detail-Abrufe
)

// SW ist ein installiertes Paket (Name + Version).
type SW struct{ Name, Version string }

// Vuln ist ein Treffer für ein Paket.
type Vuln struct {
	Package, Version, ID, Severity, Summary, Fixed, URL string
}

// Ecosystem leitet die OSV-Ökosystem-Kennung aus os/os_version ab (leer = übergreifend).
func Ecosystem(os, osVersion string) string {
	s := strings.ToLower(os + " " + osVersion)
	switch {
	case strings.Contains(s, "debian"):
		return "Debian"
	case strings.Contains(s, "ubuntu"):
		return "Ubuntu"
	case strings.Contains(s, "alpine"):
		return "Alpine"
	case strings.Contains(s, "rocky"), strings.Contains(s, "alma"), strings.Contains(s, "red hat"), strings.Contains(s, "rhel"), strings.Contains(s, "centos"):
		return "Rocky Linux"
	}
	return ""
}

// ImageEcosystem leitet die OSV-Ökosystem-Kennung eines Container-Images aus os-release
// (ID, VERSION_ID) und dem Paketmanager ab. Versionierte Kennungen (Alpine:v3.20, Debian:12,
// Ubuntu:22.04) treffen genauer als die übergreifenden. Leer = nicht abgleichbar (unbekanntes
// rpm-System) – ohne Ökosystem würde OSV Namen quer über alle Ökosysteme vergleichen.
func ImageEcosystem(os, version, pkg string) string {
	os = strings.ToLower(strings.TrimSpace(os))
	version = strings.TrimSpace(version)
	major := version
	if i := strings.Index(major, "."); i > 0 {
		major = major[:i]
	}
	minor := version
	if parts := strings.SplitN(version, ".", 3); len(parts) >= 2 {
		minor = parts[0] + "." + parts[1]
	}
	withVer := func(name, v string) string {
		if v != "" {
			return name + ":" + v
		}
		return name
	}
	switch os {
	case "alpine":
		if minor != "" {
			return "Alpine:v" + minor
		}
		return "Alpine"
	case "debian":
		return withVer("Debian", major)
	case "ubuntu":
		return withVer("Ubuntu", minor)
	case "rocky":
		return withVer("Rocky Linux", major)
	case "almalinux":
		return withVer("AlmaLinux", major)
	case "rhel", "centos":
		return "Red Hat"
	case "ol":
		return "Oracle Linux"
	case "amzn":
		return "Amazon Linux"
	case "wolfi":
		return "Wolfi"
	case "chainguard":
		return "Chainguard"
	}
	switch pkg {
	case "apk":
		return "Alpine"
	case "dpkg":
		return "Debian"
	}
	return ""
}

type osvPkg struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem,omitempty"`
}
type osvQuery struct {
	Package osvPkg `json:"package"`
	Version string `json:"version,omitempty"`
}
type batchResp struct {
	Results []struct {
		Vulns []struct {
			ID string `json:"id"`
		} `json:"vulns"`
	} `json:"results"`
}

// Known ist ein bereits bekannter Detail-Datensatz (z. B. aus früheren Scans in der DB),
// damit Details nicht je Gerät erneut von OSV geladen werden müssen.
type Known struct{ Severity, Summary, Fixed string }

// Scanner kapselt den OSV-Abgleich mit einem prozessweiten Detail-Cache. Known liefert
// optional Details aus einem persistenten Speicher (nil = nur In-Memory-Cache).
type Scanner struct {
	Known func(ctx context.Context, ids []string) map[string]Known
	mu    sync.Mutex
	cache map[string]*osvDetail
}

// Default ist der gemeinsame Scanner des Servers (Hintergrund-Scan und manueller Scan
// teilen sich den Cache).
var Default = &Scanner{}

// Scan fragt OSV.dev ab und liefert die Treffer. ecosystem darf leer sein.
func Scan(ctx context.Context, client *http.Client, sw []SW, ecosystem string) ([]Vuln, error) {
	return Default.Scan(ctx, client, sw, ecosystem)
}

func (sc *Scanner) cached(id string) (*osvDetail, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	d, ok := sc.cache[id]
	return d, ok
}

func (sc *Scanner) remember(id string, d *osvDetail) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.cache == nil {
		sc.cache = map[string]*osvDetail{}
	}
	sc.cache[id] = d
}

// Scan fragt OSV.dev ab und liefert die Treffer. ecosystem darf leer sein.
func (sc *Scanner) Scan(ctx context.Context, client *http.Client, sw []SW, ecosystem string) ([]Vuln, error) {
	if len(sw) > maxSW {
		sw = sw[:maxSW]
	}
	var queries []osvQuery
	var idx []int // Query -> sw-Index
	for i, p := range sw {
		if p.Name == "" || p.Version == "" {
			continue
		}
		queries = append(queries, osvQuery{Package: osvPkg{Name: p.Name, Ecosystem: ecosystem}, Version: p.Version})
		idx = append(idx, i)
	}
	if len(queries) == 0 {
		return nil, nil
	}

	body, _ := json.Marshal(map[string]any{"queries": queries})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, batchURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osv querybatch: HTTP %d", resp.StatusCode)
	}
	var br batchResp
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, err
	}

	// (sw-Index, vuln-ID)-Paare sammeln, Details je eindeutiger ID einmalig laden.
	type hit struct {
		swIdx int
		id    string
	}
	var hits []hit
	details := map[string]*osvDetail{}
	for qi, res := range br.Results {
		if qi >= len(idx) {
			break
		}
		for _, v := range res.Vulns {
			hits = append(hits, hit{swIdx: idx[qi], id: v.ID})
			details[v.ID] = nil
		}
	}
	ids := make([]string, 0, len(details))
	for id := range details {
		ids = append(ids, id)
	}
	// Versionierte Ökosysteme (Container-Images: Debian:13, Alpine:v3.20 …): Details immer von
	// OSV, nicht aus dem persistenten Cache der Host-Scans – dessen „Behoben in“ kann aus einem
	// anderen Release stammen.
	details, known := sc.resolve(ctx, client, ids, !strings.Contains(ecosystem, ":"))

	// Einträge ohne eigene Stufe (z. B. USN-Notizen) erben die höchste Stufe ihrer
	// verknüpften CVE-Einträge.
	inherited := map[string]string{}
	var relIDs []string
	relFor := map[string][]string{}
	for id, d := range details {
		if d == nil || d.severity() != "" {
			continue
		}
		relFor[id] = d.relatedIDs()
		relIDs = append(relIDs, relFor[id]...)
	}
	if len(relIDs) > 0 {
		relDetails, relKnown := sc.resolve(ctx, client, relIDs, true)
		for id, rels := range relFor {
			var levels []string
			for _, r := range rels {
				if d := relDetails[r]; d != nil {
					levels = append(levels, d.severity())
				} else if k, ok := relKnown[r]; ok {
					levels = append(levels, k.Severity)
				}
			}
			inherited[id] = maxLevel(levels...)
		}
	}

	var out []Vuln
	for _, h := range hits {
		p := sw[h.swIdx]
		v := Vuln{Package: p.Name, Version: p.Version, ID: h.id, URL: "https://osv.dev/vulnerability/" + h.id}
		if d := details[h.id]; d != nil {
			v.Severity = maxLevel(d.severity(), inherited[h.id])
			v.Summary = d.summaryText()
			v.Fixed = d.fixedFor(p.Name, ecosystem)
			if u := d.urgencyFor(p.Name, ecosystem); u != "" {
				v.Severity = u // Einschätzung der Distribution vor der allgemeinen CVSS-Bewertung
			}
		} else if k, ok := known[h.id]; ok {
			v.Severity, v.Summary, v.Fixed = k.Severity, k.Summary, k.Fixed
		}
		out = append(out, v)
	}
	return out, nil
}

type osvDetail struct {
	Summary  string   `json:"summary"`
	Details  string   `json:"details"`
	Aliases  []string `json:"aliases"`
	Related  []string `json:"related"`
	Upstream []string `json:"upstream"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
	Affected []struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Ranges []struct {
			Events []struct {
				Fixed string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
		DatabaseSpecific struct {
			Severity string `json:"severity"`
			Urgency  string `json:"urgency"`
		} `json:"database_specific"`
		EcosystemSpecific struct {
			Urgency string `json:"urgency"`
		} `json:"ecosystem_specific"`
	} `json:"affected"`
}

// severity leitet die Stufe ab: qualitative Angaben (GHSA database_specific, Ubuntu-
// Priority/Debian-Urgency im severity-Array) vor dem aus dem CVSS-Vektor gerechneten
// Basiswert (v2/v3; v4 kann nicht gerechnet werden).
func (d *osvDetail) severity() string {
	if s := LevelFromWord(d.DatabaseSpecific.Severity); s != "" {
		return s
	}
	for _, a := range d.Affected {
		if s := LevelFromWord(a.DatabaseSpecific.Severity); s != "" {
			return s
		}
	}
	for _, sv := range d.Severity {
		if !strings.HasPrefix(strings.ToUpper(sv.Type), "CVSS") {
			if s := LevelFromWord(sv.Score); s != "" {
				return s
			}
		}
	}
	best := 0.0
	for _, sv := range d.Severity {
		if score, ok := ScoreFromVector(sv.Score); ok && score > best {
			best = score
		}
	}
	return Level(best)
}

// fixedFor liefert die behobene Version für das Paket. Bei versioniertem Ökosystem
// (Debian:13, Alpine:v3.20 …) nur aus dem Eintrag genau dieses Releases – fehlt dort eine
// Korrektur, ist die Lücke im Release noch offen (""), auch wenn ein anderes Release sie hat.
// Ohne Eintrag für das Release (oder bei unversioniertem Ökosystem) gilt der erste Treffer.
func (d *osvDetail) fixedFor(pkg, ecosystem string) string {
	first, sawEco := "", false
	versioned := strings.Contains(ecosystem, ":")
	for _, a := range d.Affected {
		if a.Package.Name != "" && !strings.EqualFold(a.Package.Name, pkg) {
			continue
		}
		match := ecosystem != "" && strings.EqualFold(a.Package.Ecosystem, ecosystem)
		if match {
			sawEco = true
		}
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed == "" {
					continue
				}
				if match {
					return e.Fixed
				}
				if first == "" {
					first = e.Fixed
				}
			}
		}
	}
	if versioned && sawEco {
		return "" // im eigenen Release (noch) ohne Korrektur
	}
	return first
}

// urgencyFor liefert die Einschätzung der Distribution (Debian „urgency“) für das Release als
// Stufe; "" bei fehlender oder nicht vergebener Einschätzung.
func (d *osvDetail) urgencyFor(pkg, ecosystem string) string {
	if ecosystem == "" {
		return ""
	}
	for _, a := range d.Affected {
		if a.Package.Name != "" && !strings.EqualFold(a.Package.Name, pkg) {
			continue
		}
		if !strings.EqualFold(a.Package.Ecosystem, ecosystem) {
			continue
		}
		u := a.EcosystemSpecific.Urgency
		if u == "" {
			u = a.DatabaseSpecific.Urgency
		}
		return urgencyLevel(u)
	}
	return ""
}

// urgencyLevel ordnet Debian-Dringlichkeiten Stufen zu ("low*", "unimportant" …).
func urgencyLevel(u string) string {
	u = strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "*"))
	switch u {
	case "unimportant", "low", "negligible":
		return "LOW"
	case "medium":
		return "MEDIUM"
	case "high":
		return "HIGH"
	}
	return "" // "not yet assigned", "end-of-life", leer
}

func fetchDetail(ctx context.Context, client *http.Client, id string) *osvDetail {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, vulnURL+id, nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var d osvDetail
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil
	}
	return &d
}

// summaryText liefert die Kurzbeschreibung; fehlt sie (z. B. Ubuntu-CVE-Einträge), die
// erste Zeile der Langbeschreibung.
func (d *osvDetail) summaryText() string {
	if d.Summary != "" {
		return d.Summary
	}
	t := strings.TrimSpace(strings.ReplaceAll(d.Details, "\n", " "))
	if len(t) > 200 {
		t = t[:200] + "…"
	}
	return t
}

// relatedIDs sind verknüpfte Einträge (Upstream-CVE, Aliase), aus denen eine fehlende
// Stufe übernommen werden kann – z. B. USN-Notizen ohne eigene Bewertung.
func (d *osvDetail) relatedIDs() []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range [][]string{d.Upstream, d.Aliases, d.Related} {
		for _, id := range l {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

// maxLevel liefert die höchste von mehreren Stufen.
func maxLevel(levels ...string) string {
	rank := map[string]int{"": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}
	best := ""
	for _, l := range levels {
		if rank[l] > rank[best] {
			best = l
		}
	}
	return best
}

// resolve beschafft Detail-Datensätze zu IDs: 1) Prozess-Cache, 2) persistent bekannte
// Details (Known), 3) Rest parallel von OSV (begrenzt je Aufruf; Fehlendes holt der
// nächste Lauf nach). Nicht geladene IDs fehlen in beiden Ergebnissen.
func (sc *Scanner) resolve(ctx context.Context, client *http.Client, ids []string, useKnown bool) (map[string]*osvDetail, map[string]Known) {
	details := map[string]*osvDetail{}
	known := map[string]Known{}
	var missing []string
	for _, id := range ids {
		if _, dup := details[id]; dup {
			continue
		}
		if d, ok := sc.cached(id); ok {
			details[id] = d
			continue
		}
		details[id] = nil
		missing = append(missing, id)
	}
	if useKnown && sc.Known != nil && len(missing) > 0 {
		known = sc.Known(ctx, missing)
		if known == nil {
			known = map[string]Known{}
		}
		rest := missing[:0]
		for _, id := range missing {
			if _, ok := known[id]; !ok {
				rest = append(rest, id)
			}
		}
		missing = rest
	}
	if len(missing) > maxVulns {
		missing = missing[:maxVulns]
	}
	if len(missing) > 0 {
		var wg sync.WaitGroup
		sem := make(chan struct{}, fetchWorkers)
		var mu sync.Mutex
		for _, id := range missing {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				d := fetchDetail(ctx, client, id)
				if d != nil {
					sc.remember(id, d)
				}
				mu.Lock()
				details[id] = d
				mu.Unlock()
			}(id)
		}
		wg.Wait()
	}
	return details, known
}
