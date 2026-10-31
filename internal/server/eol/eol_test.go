package eol

import (
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		d       Device
		product string
		first   string
	}{
		{Device{OS: "ubuntu", OSVersion: "24.04"}, "ubuntu", "24.04"},
		{Device{OS: "debian", OSVersion: "12"}, "debian", "12"},
		{Device{OS: "raspbian", OSVersion: "12.5"}, "debian", "12"},
		{Device{OS: "rocky", OSVersion: "9.4"}, "rocky-linux", "9"},
		{Device{OS: "almalinux", OSVersion: "9.4"}, "almalinux", "9"},
		{Device{OS: "fedora", OSVersion: "40"}, "fedora", "40"},
		{Device{OS: "alpine", OSVersion: "3.20.1"}, "alpine-linux", "3.20"},
		{Device{OS: "opensuse-leap", OSVersion: "15.6"}, "opensuse", "15.6"},
		{Device{OS: "freebsd", OSVersion: "14.1-RELEASE"}, "freebsd", "14"},
		{Device{OS: "darwin", OSVersion: "14.5"}, "macos", "14"},
		{Device{OS: "Microsoft Windows 11 Pro", OSVersion: "10.0.22631 Build 22631", OSBuild: "23H2 (22631.4169)", OSCodename: "23H2", OSEdition: "Professional"}, "windows", "11-23h2-w"},
		{Device{OS: "Microsoft Windows 11 Enterprise", OSVersion: "10.0.26100 Build 26100", OSCodename: "24H2", OSEdition: "Enterprise"}, "windows", "11-24h2-e"},
		{Device{OS: "Microsoft Windows 10 Enterprise LTSC", OSVersion: "10.0.19044 Build 19044", OSCodename: "21H2", OSEdition: "EnterpriseS"}, "windows", "10-21h2-e-lts"},
		{Device{OS: "Microsoft Windows 10 Pro", OSVersion: "10.0.19045 Build 19045"}, "windows", "10-22h2-w"},
		{Device{OS: "Microsoft Windows Server 2022 Standard", OSVersion: "10.0.20348 Build 20348", OSEdition: "ServerStandard"}, "windows-server", "2022"},
	}
	for _, c := range cases {
		ts := Resolve(c.d)
		if len(ts) == 0 {
			t.Errorf("%+v: kein Ziel", c.d)
			continue
		}
		if ts[0].Product != c.product || ts[0].Candidates[0] != c.first {
			t.Errorf("%+v: got %s %v want %s %s", c.d, ts[0].Product, ts[0].Candidates, c.product, c.first)
		}
	}
	for _, rolling := range []string{"arch", "cachyos", "manjaro", "gentoo", "nixos"} {
		if ts := Resolve(Device{OS: rolling, OSVersion: ""}); len(ts) != 0 {
			t.Errorf("%s: rolling darf kein Ziel haben: %+v", rolling, ts)
		}
	}
	// Windows 10 22H2 hat nur einen Zyklus "10-22h2" → zweiter Kandidat greift.
	ts := Resolve(Device{OS: "Microsoft Windows 10 Pro", OSVersion: "10.0.19045 Build 19045"})
	if c, ok := Match([]Cycle{{Name: "10-22h2"}}, ts[0].Candidates); !ok || c.Name != "10-22h2" {
		t.Errorf("Fallback-Kandidat: %v %v", c, ok)
	}
	// Proxmox-Host liefert zusätzlich proxmox-ve.
	ts = Resolve(Device{OS: "debian", OSVersion: "12", ProxmoxVersion: "8.2.4"})
	if len(ts) != 2 || ts[1].Product != "proxmox-ve" || ts[1].Candidates[0] != "8" || ts[1].Kind != "proxmox" {
		t.Errorf("proxmox: %+v", ts)
	}
}

func TestParseAndClassify(t *testing.T) {
	body := []byte(`{"schema_version":"1.2.0","result":{"name":"ubuntu","releases":[
	 {"name":"24.04","codename":"Noble Numbat","label":"24.04 'Noble Numbat' (LTS)","releaseDate":"2024-04-25","isLts":true,"isEoas":false,"eoasFrom":"2029-04-25","isEol":false,"eolFrom":"2029-04-25","isMaintained":true,"latest":{"name":"24.04.1"}},
	 {"name":"20.04","codename":"Focal Fossa","label":"20.04 (LTS)","releaseDate":"2020-04-23","isLts":true,"isEoas":true,"eoasFrom":"2025-04-02","isEol":true,"eolFrom":"2025-05-29","isMaintained":false},
	 {"name":"23.10","label":"23.10","releaseDate":"2023-10-12","isLts":false,"isEol":true,"eolFrom":"2024-07-11"}]}}`)
	cycles, err := Parse(body)
	if err != nil || len(cycles) != 3 {
		t.Fatalf("parse: %v %d", err, len(cycles))
	}
	noble, ok := Match(cycles, []string{"24.04"})
	if !ok || noble.EOL == nil || noble.EOL.Format("2006-01-02") != "2029-04-25" || !noble.LTS || noble.Latest != "24.04.1" {
		t.Fatalf("noble: %+v", noble)
	}
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if s := Classify(noble, now, 90); s != StatusOK {
		t.Errorf("noble: %s", s)
	}
	focal, _ := Match(cycles, []string{"20.04"})
	if s := Classify(focal, now, 90); s != StatusEOL {
		t.Errorf("focal: %s", s)
	}
	if s := Classify(noble, time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC), 90); s != StatusSoon {
		t.Errorf("soon: %s", s)
	}
	if s := Classify(Cycle{Name: "x"}, now, 90); s != StatusUnknown {
		t.Errorf("unknown: %s", s)
	}
	if s := Classify(Cycle{Name: "x", IsEOL: true}, now, 90); s != StatusEOL {
		t.Errorf("iseol: %s", s)
	}
	if _, err := Parse([]byte(`{"result":{"releases":[]}}`)); err == nil {
		t.Error("leere Antwort muss Fehler sein")
	}
}
