package collect

import "testing"

func TestParseAptUpgradable(t *testing.T) {
	out := `Listing... Done
bash/jammy-updates,jammy-security 5.1-6ubuntu1.1 amd64 [upgradable from: 5.1-6ubuntu1]
curl/jammy-updates 7.81.0-1ubuntu1.16 amd64 [upgradable from: 7.81.0-1ubuntu1.15]
libssl3/bookworm-security 3.0.14-1~deb12u2 amd64 [upgradable from: 3.0.13-1~deb12u1]
`
	items := parseAptUpgradable(out)
	if len(items) != 3 {
		t.Fatalf("items=%d", len(items))
	}
	b := items[0]
	if b.Name != "bash" || b.Version != "5.1-6ubuntu1.1" || b.CurrentVersion != "5.1-6ubuntu1" || !b.Security || b.Severity != "Important" {
		t.Errorf("bash: %+v", b)
	}
	if b.Source != "jammy-updates,jammy-security" {
		t.Errorf("source=%q", b.Source)
	}
	if items[1].Security || items[1].Severity != "Other" {
		t.Errorf("curl darf kein Sicherheits-Update sein: %+v", items[1])
	}
	if !items[2].Security || items[2].Name != "libssl3" {
		t.Errorf("libssl3: %+v", items[2])
	}
	if securityCount(items) != 2 {
		t.Errorf("securityCount=%d", securityCount(items))
	}
}

func TestParseDnf(t *testing.T) {
	adv := parseDnfUpdateinfo(`Last metadata expiration check: 0:10:00 ago on Mon 01 Jan 2024.
FEDORA-2024-1a2b3c4d Important/Sec. bash-5.2.26-3.fc40.x86_64
FEDORA-2024-99999999 Critical/Sec.  openssl-libs-1:3.2.1-3.fc40.x86_64
FEDORA-2024-abcdef00 Moderate/Sec.  bash-5.2.26-3.fc40.x86_64
FEDORA-2024-bugfix   bugfix         vim-enhanced-9.1.0-1.fc40.x86_64
`)
	if a, ok := adv["bash"]; !ok || a.Severity != "Important" || a.ID != "FEDORA-2024-1a2b3c4d" {
		t.Errorf("bash advisory: %+v %v", a, ok)
	}
	if a, ok := adv["openssl-libs"]; !ok || a.Severity != "Critical" {
		t.Errorf("openssl advisory: %+v %v", a, ok)
	}
	if _, ok := adv["vim-enhanced"]; ok {
		t.Errorf("bugfix darf nicht als Security zählen")
	}
	items := parseDnfCheckUpdate(`
bash.x86_64          5.2.26-3.fc40       updates
openssl-libs.x86_64  1:3.2.1-3.fc40      updates
vim-enhanced.x86_64  2:9.1.0-1.fc40      updates
Obsoleting Packages
foo.noarch           1.0-1.fc40          updates
`, adv)
	if len(items) != 4 {
		t.Fatalf("items=%d: %+v", len(items), items)
	}
	if items[0].Name != "bash" || !items[0].Security || items[0].Severity != "Important" || items[0].Version != "5.2.26-3.fc40" {
		t.Errorf("bash: %+v", items[0])
	}
	if items[0].Source != "updates FEDORA-2024-1a2b3c4d" {
		t.Errorf("source=%q", items[0].Source)
	}
	if items[1].Severity != "Critical" {
		t.Errorf("openssl: %+v", items[1])
	}
	if items[2].Security {
		t.Errorf("vim: %+v", items[2])
	}
}

func TestRpmName(t *testing.T) {
	cases := map[string]string{
		"bash-5.2.26-3.fc40.x86_64":          "bash",
		"openssl-libs-1:3.2.1-3.fc40.x86_64": "openssl-libs",
		"kernel-core-6.8.9-300.fc40.x86_64":  "kernel-core",
		"python3-pip-23.2.1-2.fc40.noarch":   "python3-pip",
	}
	for in, want := range cases {
		if got := rpmName(in); got != want {
			t.Errorf("rpmName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParsePacmanAndApk(t *testing.T) {
	p := parsePacmanUpgrades("bash 5.2.026-2 -> 5.2.026-3\nlinux 6.9.3.arch1-1 -> 6.9.5.arch1-1 [ignored]\ncurl 8.8.0-1 -> 8.8.0-2\n")
	if len(p) != 2 || p[0].Name != "bash" || p[0].CurrentVersion != "5.2.026-2" || p[0].Version != "5.2.026-3" {
		t.Errorf("pacman: %+v", p)
	}
	a := parseApkUpgradable("Installed:                                Available:\nbash-5.2.26-r0                             < 5.2.26-r1\nfetch https://dl-cdn.alpinelinux.org/x86_64/APKINDEX.tar.gz\nmusl-utils-1.2.5-r0                        < 1.2.5-r1\n")
	if len(a) != 2 || a[0].Name != "bash" || a[0].CurrentVersion != "5.2.26-r0" || a[0].Version != "5.2.26-r1" || a[1].Name != "musl-utils" {
		t.Errorf("apk: %+v", a)
	}
}

func TestParseSoftwareUpdateList(t *testing.T) {
	out := `Software Update Tool

Finding available software
Software Update found the following new or updated software:
* Label: macOS Sonoma 14.6.1-23G93
	Title: macOS Sonoma 14.6.1, Version: 14.6.1, Size: 1234567KiB, Recommended: YES, Action: restart,
* Label: Safari17.6VenturaAuto-17.6
	Title: Safari, Version: 17.6, Size: 150000KiB, Recommended: YES,
* Label: Rapid Security Response 14.6.1 (a)-23G93a
	Title: Rapid Security Response, Version: 14.6.1 (a), Size: 1000KiB, Recommended: YES, Action: restart,
`
	items := parseSoftwareUpdateList(out)
	if len(items) != 3 {
		t.Fatalf("items=%d", len(items))
	}
	if items[0].Name != "macOS Sonoma 14.6.1-23G93" || items[0].Version != "14.6.1" || items[0].Source != "recommended" || items[0].Security {
		t.Errorf("sonoma: %+v", items[0])
	}
	if !items[2].Security || items[2].Severity != "Important" {
		t.Errorf("rsr: %+v", items[2])
	}
}

func TestClassifyWindowsUpdate(t *testing.T) {
	if sec, kind := classifyWindowsUpdate([]string{"Security Updates", "Windows 11"}, "Important"); !sec || kind != "" {
		t.Errorf("security: %v %q", sec, kind)
	}
	if sec, kind := classifyWindowsUpdate([]string{"Definition Updates", "Microsoft Defender Antivirus"}, ""); sec || kind != "definition" {
		t.Errorf("definition: %v %q", sec, kind)
	}
	if sec, kind := classifyWindowsUpdate([]string{"Drivers"}, ""); sec || kind != "driver" {
		t.Errorf("driver: %v %q", sec, kind)
	}
	if sec, _ := classifyWindowsUpdate([]string{"Updates"}, "Moderate"); !sec {
		t.Errorf("msrc severity muss als security zählen")
	}
}
