package collect

import "testing"

func TestParseAptSources(t *testing.T) {
	list := `# Ubuntu
deb http://de.archive.ubuntu.com/ubuntu/ jammy main restricted
deb-src http://de.archive.ubuntu.com/ubuntu/ jammy main restricted
# deb http://de.archive.ubuntu.com/ubuntu/ jammy-proposed main
deb [arch=amd64 signed-by=/usr/share/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu jammy stable
deb [trusted=yes] file:/opt/local-repo ./
`
	r := parseAptSourcesList(list, "sources.list")
	if len(r) != 4 {
		t.Fatalf("repos=%d: %+v", len(r), r)
	}
	if r[0].URL != "http://de.archive.ubuntu.com/ubuntu/" || r[0].Suite != "jammy main restricted" || r[0].ThirdParty || !r[0].Enabled {
		t.Errorf("ubuntu: %+v", r[0])
	}
	if r[1].Enabled || r[1].Name != "jammy-proposed" {
		t.Errorf("proposed: %+v", r[1])
	}
	if !r[2].ThirdParty || r[2].URL != "https://download.docker.com/linux/ubuntu" || !r[2].GPGCheck {
		t.Errorf("docker: %+v", r[2])
	}
	if r[3].GPGCheck || r[3].ThirdParty {
		t.Errorf("trusted local: %+v", r[3])
	}
	d := parseDeb822Sources(`Types: deb
URIs: http://deb.debian.org/debian http://security.debian.org/debian-security
Suites: bookworm bookworm-updates
Components: main contrib
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: https://packages.microsoft.com/repos/code
Suites: stable
Components: main
Enabled: no
`, "x.sources")
	if len(d) != 3 || d[0].URL != "http://deb.debian.org/debian" || d[0].ThirdParty || d[0].Suite != "bookworm bookworm-updates main contrib" {
		t.Fatalf("deb822: %+v", d)
	}
	if d[2].Enabled || !d[2].ThirdParty || d[2].ID != "x.sources#2" {
		t.Errorf("vscode: %+v", d[2])
	}
}

func TestParseIniAndPacmanAndApk(t *testing.T) {
	ini := parseIniRepos(`[fedora]
name=Fedora $releasever - $basearch
metalink=https://mirrors.fedoraproject.org/metalink?repo=fedora-$releasever&arch=$basearch
enabled=1
gpgcheck=1

[google-chrome]
name=google-chrome
baseurl=https://dl.google.com/linux/chrome/rpm/stable/x86_64
enabled=0
gpgcheck=0
`, "dnf")
	if len(ini) != 2 || ini[0].ThirdParty || !ini[0].Enabled || ini[1].Enabled || ini[1].GPGCheck || !ini[1].ThirdParty {
		t.Fatalf("ini: %+v", ini)
	}
	pac := parsePacmanConf(`[options]
HoldPkg = pacman glibc

[cachyos-v3]
Include = /etc/pacman.d/cachyos-v3-mirrorlist

[core]
Include = /etc/pacman.d/mirrorlist

#[multilib]
#Include = /etc/pacman.d/mirrorlist

#[repo-name]
#Server = ServerName
#Include = IncludePath

#[custom]
#SigLevel = Optional TrustAll
#Server = file:///home/custompkgs

[chaotic-aur]
SigLevel = Never
Server = https://geo-mirror.chaotic.cx/$repo/$arch
`, func(path string) string {
		if path == "/etc/pacman.d/mirrorlist" {
			return "## Germany\nServer = https://mirror.example.org/archlinux/$repo/os/$arch\n"
		}
		return ""
	})
	if len(pac) != 4 {
		t.Fatalf("pacman=%d: %+v", len(pac), pac)
	}
	if pac[1].ID != "core" || pac[1].URL != "https://mirror.example.org/archlinux/$repo/os/$arch" || pac[1].ThirdParty {
		t.Errorf("core: %+v", pac[1])
	}
	if pac[0].URL != "include:/etc/pacman.d/cachyos-v3-mirrorlist" || pac[0].ThirdParty {
		t.Errorf("cachyos: %+v", pac[0])
	}
	if pac[2].Enabled || pac[2].ID != "multilib" {
		t.Errorf("multilib: %+v", pac[2])
	}
	if !pac[3].ThirdParty || pac[3].GPGCheck {
		t.Errorf("chaotic: %+v", pac[3])
	}
	apk := parseApkRepositories("https://dl-cdn.alpinelinux.org/alpine/v3.20/main\n#https://dl-cdn.alpinelinux.org/alpine/v3.20/community\n@edge https://dl-cdn.alpinelinux.org/alpine/edge/testing\n")
	if len(apk) != 3 || apk[0].Name != "main" || apk[1].Enabled || apk[2].Name != "testing" || apk[0].ThirdParty {
		t.Fatalf("apk: %+v", apk)
	}
	wg := parseWingetSources("Name    Argument\n-----------------------------------------\nmsstore https://storeedgefd.dsx.mp.microsoft.com/v9.0\nwinget  https://cdn.winget.microsoft.com/cache\n")
	if len(wg) != 2 || wg[1].ID != "winget" || wg[1].ThirdParty {
		t.Fatalf("winget: %+v", wg)
	}
}

func TestAutoUpdateStatus(t *testing.T) {
	auto := "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	unatt := `Unattended-Upgrade::Allowed-Origins {
	"${distro_id}:${distro_codename}-security";
//	"${distro_id}:${distro_codename}-updates";
};`
	if s := aptAutoUpdateStatus(auto, unatt, true); s != "security" {
		t.Errorf("apt security: %q", s)
	}
	if s := aptAutoUpdateStatus(auto, `	"${distro_id}:${distro_codename}-updates";`, true); s != "enabled" {
		t.Errorf("apt enabled: %q", s)
	}
	if s := aptAutoUpdateStatus("APT::Periodic::Unattended-Upgrade \"0\";", unatt, true); s != "disabled" {
		t.Errorf("apt off: %q", s)
	}
	if s := aptAutoUpdateStatus(auto, unatt, false); s != "disabled" {
		t.Errorf("apt timer off: %q", s)
	}
	if s := dnfAutomaticStatus("[commands]\nupgrade_type = security\napply_updates = yes\n", true); s != "security" {
		t.Errorf("dnf security: %q", s)
	}
	if s := dnfAutomaticStatus("apply_updates = no\ndownload_updates = yes\n", true); s != "download" {
		t.Errorf("dnf download: %q", s)
	}
	if s := dnfAutomaticStatus("", false); s != "disabled" {
		t.Errorf("dnf off: %q", s)
	}
	if s := winAutoUpdateStatus(4, false); s != "enabled" {
		t.Errorf("win: %q", s)
	}
	if s := winAutoUpdateStatus(4, true); s != "disabled" {
		t.Errorf("win policy: %q", s)
	}
}

func TestKernelCompare(t *testing.T) {
	if !kernelVersionLess("6.8.0-45-generic", "6.8.0-47-generic") || kernelVersionLess("6.17.5-2-cachyos", "6.9.3-arch1-1") {
		t.Error("kernelVersionLess")
	}
	if v := newestKernel([]string{"6.8.0-45-generic", "6.8.0-47-generic", "5.15.0-100-generic", ""}); v != "6.8.0-47-generic" {
		t.Errorf("newest: %q", v)
	}
	if !kernelUpgradePending("6.8.0-45-generic", "6.8.0-47-generic") || kernelUpgradePending("6.8.0-47-generic", "6.8.0-47-generic") || kernelUpgradePending("6.8.0-47-generic", "6.8.0-45-generic") {
		t.Error("kernelUpgradePending")
	}
	if osReleaseField("NAME=\"Ubuntu\"\nVERSION_CODENAME=jammy\n", "VERSION_CODENAME") != "jammy" {
		t.Error("osReleaseField")
	}
}
