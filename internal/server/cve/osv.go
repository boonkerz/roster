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
)

const (
	batchURL = "https://api.osv.dev/v1/querybatch"
	vulnURL  = "https://api.osv.dev/v1/vulns/"
	maxSW    = 500 // Obergrenze abgefragter Pakete je Gerät
	maxVulns = 300 // Obergrenze aufgelöster Detail-Datensätze je Scan
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

// Scan fragt OSV.dev ab und liefert die Treffer. ecosystem darf leer sein.
func Scan(ctx context.Context, client *http.Client, sw []SW, ecosystem string) ([]Vuln, error) {
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
	fetched := 0
	for id := range details {
		if fetched >= maxVulns {
			break
		}
		details[id] = fetchDetail(ctx, client, id)
		fetched++
	}

	var out []Vuln
	for _, h := range hits {
		p := sw[h.swIdx]
		v := Vuln{Package: p.Name, Version: p.Version, ID: h.id, URL: "https://osv.dev/vulnerability/" + h.id}
		if d := details[h.id]; d != nil {
			v.Severity = d.severity()
			v.Summary = d.Summary
			v.Fixed = d.fixedFor(p.Name)
		}
		out = append(out, v)
	}
	return out, nil
}

type osvDetail struct {
	Summary  string `json:"summary"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
	Affected []struct {
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
		Ranges []struct {
			Events []struct {
				Fixed string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
		DatabaseSpecific struct {
			Severity string `json:"severity"`
		} `json:"database_specific"`
	} `json:"affected"`
}

func (d *osvDetail) severity() string {
	if s := strings.ToUpper(d.DatabaseSpecific.Severity); s != "" {
		return s
	}
	for _, a := range d.Affected {
		if s := strings.ToUpper(a.DatabaseSpecific.Severity); s != "" {
			return s
		}
	}
	// CVSS-Vektor -> grobe Stufe über den Basiswert im Vektor (falls als Zahl anhängt).
	for _, sv := range d.Severity {
		if sv.Score != "" {
			return "" // Vektor ohne einfachen Score – Stufe unbekannt lassen
		}
	}
	return ""
}

func (d *osvDetail) fixedFor(pkg string) string {
	for _, a := range d.Affected {
		if a.Package.Name != "" && !strings.EqualFold(a.Package.Name, pkg) {
			continue
		}
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
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
