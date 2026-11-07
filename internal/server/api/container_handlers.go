package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/cve"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// Container-Sicherheit: OSV-Abgleich der Image-Paketlisten und Datenbank-Abweichungen.

func (s *Server) handleDeviceContainerVulns(w http.ResponseWriter, r *http.Request) {
	vulns, err := s.store.ContainerVulnsFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if vulns == nil {
		vulns = []model.ContainerVulnerability{}
	}
	s.writeJSON(w, http.StatusOK, vulns)
}

func (s *Server) handleAllContainerVulns(w http.ResponseWriter, r *http.Request) {
	vulns, err := s.store.AllContainerVulns(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if vulns == nil {
		vulns = []model.ContainerVulnerability{}
	}
	s.writeJSON(w, http.StatusOK, vulns)
}

// handleScanImages lässt den Agent die Image-Paketlisten sofort neu lesen; der OSV-Abgleich
// folgt automatisch nach dem nächsten Checkin.
func (s *Server) handleScanImages(w http.ResponseWriter, r *http.Request) {
	s.queueDeviceCmd(w, r, "scan_images", "Container-Images neu scannen", nil)
}

// kickImageVulns stößt den OSV-Abgleich an (nicht blockierend).
func (s *Server) kickImageVulns() {
	select {
	case s.imageVulnKick <- struct{}{}:
	default:
	}
}

// RunImageVulnLoop gleicht ausstehende Image-Paketlisten gegen OSV.dev ab: nach jedem Checkin
// mit neuen Scans (verzögert gebündelt), sonst alle 30 min für den täglichen Neuabgleich.
func (s *Server) RunImageVulnLoop(ctx context.Context) {
	client := &http.Client{Timeout: 20 * time.Second}
	run := func() {
		for i := 0; i < 50; i++ { // Schleife bis nichts mehr aussteht (max. 50 Runden à 20 Images)
			pending, err := s.store.PendingImageScans(ctx, 20)
			if err != nil || len(pending) == 0 {
				return
			}
			for _, p := range pending {
				if ctx.Err() != nil {
					return
				}
				eco := cve.ImageEcosystem(p.OS, p.OSVersion, p.Pkg)
				var vulns []model.ContainerVulnerability
				if eco != "" {
					sw := make([]cve.SW, 0, len(p.Packages))
					for _, pk := range p.Packages {
						sw = append(sw, cve.SW{Name: pk.Name, Version: pk.Version})
					}
					found, err := cve.Scan(ctx, client, sw, eco)
					if err != nil {
						s.log.Warn("container-image osv-abgleich fehlgeschlagen", "image", p.Image, "err", err)
						time.Sleep(30 * time.Second)
						continue
					}
					for _, v := range found {
						vulns = append(vulns, model.ContainerVulnerability{Image: p.Image, Package: v.Package, Version: v.Version, VulnID: v.ID,
							Severity: v.Severity, Summary: v.Summary, Fixed: v.Fixed, URL: v.URL})
					}
				}
				if err := s.store.ReplaceContainerVulns(ctx, p.DeviceID, p.ImageID, vulns); err != nil {
					s.log.Error("container-schwachstellen speichern", "err", err)
					return
				}
				time.Sleep(2 * time.Second) // schonend zu OSV.dev
			}
		}
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Minute):
	}
	run()
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		case <-s.imageVulnKick:
			time.Sleep(20 * time.Second) // weitere Checkins derselben Minute bündeln
			run()
		}
	}
}

// --- Datenbank-Abweichungen ---

// systemDBs sind Engine-eigene Datenbanken, deren Auftauchen/Verschwinden kein Befund ist.
var systemDBs = map[string]bool{"admin": true, "config": true, "local": true, "postgres": true}

// ransomDBName erkennt typische Namen von Erpresser-Datenbanken/-Hinweisen.
var ransomDBName = regexp.MustCompile(`(?i)(read_?me|warning|recover|restore|ransom|hacked|pwned|please_?read|how_?to|backup_?your|deleted|security_?alert|bitcoin|btc|contact)`)

// dbDiff ist das Ergebnis des Vergleichs für einen Container.
type dbDiff struct {
	Container      string
	Added, Removed []string
	Emptied        []string // Datenbanken, die ihre Collections/Tabellen verloren haben
	Critical       bool
}

// diffDockerDBs vergleicht alten und neuen Datenbank-Bestand je Container (rein, getestet).
// Nur Container, die in beiden Ständen fehlerfrei vorkommen, werden bewertet.
func diffDockerDBs(old []model.DockerDatabase, cur []shared.DockerDatabase) []dbDiff {
	type dbs struct {
		cols map[string]int
		err  bool
	}
	group := func(add func(m map[string]*dbs)) map[string]*dbs {
		m := map[string]*dbs{}
		add(m)
		return m
	}
	oldM := group(func(m map[string]*dbs) {
		for _, d := range old {
			e := m[d.Container]
			if e == nil {
				e = &dbs{cols: map[string]int{}}
				m[d.Container] = e
			}
			if d.Error != "" {
				e.err = true
			} else {
				e.cols[d.Name] = d.Collections
			}
		}
	})
	curM := group(func(m map[string]*dbs) {
		for _, d := range cur {
			e := m[d.Container]
			if e == nil {
				e = &dbs{cols: map[string]int{}}
				m[d.Container] = e
			}
			if d.Error != "" {
				e.err = true
			} else {
				e.cols[d.Name] = d.Collections
			}
		}
	})
	var out []dbDiff
	for name, o := range oldM {
		c := curM[name]
		if c == nil || o.err || c.err || len(o.cols) == 0 {
			continue
		}
		var d dbDiff
		d.Container = name
		for n, cnt := range c.cols {
			if _, ok := o.cols[n]; !ok && !systemDBs[n] {
				d.Added = append(d.Added, n)
				if ransomDBName.MatchString(n) {
					d.Critical = true
				}
				_ = cnt
			}
		}
		for n, cnt := range o.cols {
			if _, ok := c.cols[n]; !ok && !systemDBs[n] {
				d.Removed = append(d.Removed, n)
				if cnt > 0 {
					d.Critical = true
				}
			}
		}
		for n, oc := range o.cols {
			if nc, ok := c.cols[n]; ok && oc > 0 && (nc == 0 || (oc >= 4 && nc*2 < oc)) {
				d.Emptied = append(d.Emptied, fmt.Sprintf("%s (%d → %d)", n, oc, nc))
				d.Critical = true
			}
		}
		if len(d.Added)+len(d.Removed)+len(d.Emptied) == 0 {
			continue
		}
		if len(d.Added) > 0 && len(d.Removed) > 0 { // Muster „Daten weg, Hinweis-DB da“
			d.Critical = true
		}
		sort.Strings(d.Added)
		sort.Strings(d.Removed)
		sort.Strings(d.Emptied)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Container < out[j].Container })
	return out
}

// alertDBChanges meldet Datenbank-Abweichungen eines Checkins als Vorfall „db_change“.
func (s *Server) alertDBChanges(ctx context.Context, device *model.Device, old []model.DockerDatabase, cur []shared.DockerDatabase) *model.SecurityIncident {
	diffs := diffDockerDBs(old, cur)
	if len(diffs) == 0 {
		return nil
	}
	severity := "warning"
	var lines, names []string
	for _, d := range diffs {
		names = append(names, d.Container)
		if d.Critical {
			severity = "critical"
		}
		for _, n := range d.Added {
			tag := ""
			if ransomDBName.MatchString(n) {
				tag = " ⚠ Name wie bei Erpressung"
			}
			lines = append(lines, fmt.Sprintf("  • %s: neue Datenbank %q%s", d.Container, n, tag))
		}
		for _, n := range d.Removed {
			lines = append(lines, fmt.Sprintf("  • %s: Datenbank %q gelöscht", d.Container, n))
		}
		for _, n := range d.Emptied {
			lines = append(lines, fmt.Sprintf("  • %s: Datenbank %s geleert", d.Container, n))
		}
	}
	inc := &model.SecurityIncident{DeviceID: device.ID, TS: time.Now(), Kind: "db_change", Severity: severity, Status: "open",
		Title:  "Datenbank-Abweichung in " + strings.Join(names, ", "),
		Detail: strings.Join(lines, "\n") + "\n\nBestand je Datenbank-Container wird bei jedem Checkin verglichen. Gelöschte oder geleerte Datenbanken zusammen mit einer neuen Hinweis-Datenbank sind das typische Muster einer Datenbank-Erpressung – Backups und Zugriffe prüfen."}
	applySeverity(s.securitySettings(ctx), inc)
	if err := s.store.InsertIncident(ctx, inc); err != nil {
		s.log.Error("db-vorfall speichern", "err", err)
		return nil
	}
	return inc
}

// handleFleetContainers liefert die Flottenübersicht der Container-Sicherheit (Sicherheit → Container).
func (s *Server) handleFleetContainers(w http.ResponseWriter, r *http.Request) {
	images, err := s.store.FleetImages(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	dbs, err := s.store.FleetDBs(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	sort.SliceStable(images, func(i, j int) bool {
		a, b := images[i], images[j]
		if a.FixableCH != b.FixableCH { // erst, was ein neues Image wirklich behebt
			return a.FixableCH > b.FixableCH
		}
		if a.Critical != b.Critical {
			return a.Critical > b.Critical
		}
		if a.High != b.High {
			return a.High > b.High
		}
		if a.Vulns != b.Vulns {
			return a.Vulns > b.Vulns
		}
		return a.Image < b.Image
	})
	if images == nil {
		images = []store.FleetImageRow{}
	}
	if dbs == nil {
		dbs = []store.FleetDBRow{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"images": images, "databases": dbs})
}
