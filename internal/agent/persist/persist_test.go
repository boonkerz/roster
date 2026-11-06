package persist

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPkgVersion(t *testing.T) {
	s := sum([]byte("x"))
	if v := pkgVersion("0.20.2", s); v != "0.20.2" {
		t.Fatal(v)
	}
	if v := pkgVersion("v1.2.3", s); v != "1.2.3" {
		t.Fatal(v)
	}
	if v := pkgVersion("dev", s); !strings.HasPrefix(v, "0.0.0.d") || len(v) != len("0.0.0.d")+8 {
		t.Fatal(v)
	}
}

func TestManifestAndVersionJSON(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(Manifest("0.20.2", "FreeBSD:*:amd64")), &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != PkgName || m["abi"] != "FreeBSD:*:amd64" || m["prefix"] != "/usr/local" {
		t.Fatalf("%v", m)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(VersionJSON("0.20.2")), &v); err != nil {
		t.Fatal(err)
	}
	// register.php erkennt Plugins am Dateinamen = product_id ohne „os-“.
	if v["product_id"] != PkgName || !strings.HasSuffix(VersionPath, "/"+strings.TrimPrefix(PkgName, "os-")) {
		t.Fatalf("%v %s", v, VersionPath)
	}
	for _, f := range []string{BinPath, RCPath, VersionPath} {
		if !strings.Contains(Plist, strings.TrimPrefix(f, "/usr/local/")+"\n") {
			t.Errorf("plist fehlt %s", f)
		}
	}
}
