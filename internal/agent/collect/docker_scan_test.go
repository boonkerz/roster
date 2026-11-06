package collect

import (
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestParseImageScanApk(t *testing.T) {
	raw := "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.20.3\nPRETTY_NAME=\"Alpine Linux v3.20\"\n@@PKG@@\napk\nmusl-1.2.5-r0\nopenssh-server-9.7_p1-r4\nopenssh-sftp-server-9.7_p1-r4\nlibcrypto3-3.3.2-r0\nbusybox-1.36.1-r29\nkaputt\n"
	sc := parseImageScan(raw)
	if sc.OS != "alpine" || sc.OSVersion != "3.20.3" || sc.Pkg != "apk" {
		t.Fatalf("os: %+v", sc)
	}
	if len(sc.Packages) != 5 || sc.Packages[1].Name != "openssh-server" || sc.Packages[1].Version != "9.7_p1-r4" {
		t.Fatalf("pakete: %+v", sc.Packages)
	}
	if len(sc.Services) != 2 || sc.Services[0] != "sftp" || sc.Services[1] != "sshd" {
		t.Fatalf("services: %v", sc.Services)
	}
}

func TestParseImageScanDpkgRpm(t *testing.T) {
	raw := "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\n@@PKG@@\ndpkg\nlibc6:amd64 2.36-9+deb12u8 installed\nopenssh-server 1:9.2p1-2+deb12u3 installed\nold-pkg 1.0 config-files\n"
	sc := parseImageScan(raw)
	if sc.OS != "debian" || sc.OSVersion != "12" || len(sc.Packages) != 2 || sc.Packages[0].Name != "libc6" || sc.Packages[1].Version != "1:9.2p1-2+deb12u3" {
		t.Fatalf("dpkg: %+v", sc)
	}
	if len(sc.Services) != 1 || sc.Services[0] != "sshd" {
		t.Fatalf("services: %v", sc.Services)
	}
	raw = "ID=\"rocky\"\nVERSION_ID=\"9.4\"\n@@PKG@@\nrpm\nopenssl-libs 1:3.0.7-28.el9_4\nbash 5.1.8-9.el9\ngpg-pubkey 350d275d-62d7a3a9\n"
	sc = parseImageScan(raw)
	if sc.OS != "rocky" || len(sc.Packages) != 2 || sc.Packages[0].Version != "1:3.0.7-28.el9_4" {
		t.Fatalf("rpm: %+v", sc)
	}
	// Distroless: kein Paketmanager, kein os-release
	sc = parseImageScan("@@PKG@@\nnone\n")
	if sc.Pkg != "none" || len(sc.Packages) != 0 {
		t.Fatalf("none: %+v", sc)
	}
	if sc := parseImageScan(""); sc.Pkg != "" {
		t.Fatalf("leer: %+v", sc)
	}
}

func TestApkSplit(t *testing.T) {
	cases := map[string][2]string{
		"musl-1.2.5-r0":                      {"musl", "1.2.5-r0"},
		"openssh-server-9.7_p1-r4":           {"openssh-server", "9.7_p1-r4"},
		"ca-certificates-bundle-20240705-r0": {"ca-certificates-bundle", "20240705-r0"},
		"py3-pip-24.0-r2":                    {"py3-pip", "24.0-r2"},
	}
	for in, want := range cases {
		n, v, ok := apkSplit(in)
		if !ok || n != want[0] || v != want[1] {
			t.Errorf("%s -> %q %q (%v)", in, n, v, ok)
		}
	}
	if _, _, ok := apkSplit("WARNING: something"); ok {
		t.Error("warnzeile akzeptiert")
	}
}

func TestDBEngine(t *testing.T) {
	cases := map[string]string{
		"mongo:5": "mongo", "mongo:latest": "mongo", "registry.gitlab.com/x/docker_mysql:latest": "mysql",
		"mariadb": "mysql", "postgres:17-alpine": "postgres", "mongo-express": "", "nginx:1.27-alpine": "",
		"prom/mysqld-exporter": "", "percona/percona-server:8": "mysql", "timescale/timescaledb:latest-pg16": "postgres",
	}
	for in, want := range cases {
		if got := dbEngine(in); got != want {
			t.Errorf("%s: %q, erwartet %q", in, got, want)
		}
	}
}

func TestParseDBList(t *testing.T) {
	c := shared.DockerContainer{ContainerID: "abc", Name: "shop_db"}
	list := parseDBList(c, "mongo", "shop\t12\nadmin\t1\nlocal\t3\nmysql: [Warning] Using a password on the command line interface can be insecure.\nREADME_TO_RECOVER\t1\n")
	if len(list) != 4 || list[0].Name != "README_TO_RECOVER" || list[3].Name != "shop" || list[3].Collections != 12 || list[0].Container != "shop_db" || list[0].Engine != "mongo" {
		t.Fatalf("liste: %+v", list)
	}
	if len(parseDBList(c, "mysql", "")) != 0 {
		t.Fatal("leer")
	}
}
