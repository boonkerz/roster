package collect

import "testing"

func TestParseDockerContainers(t *testing.T) {
	// Zwei Zeilen wie `docker ps --all --format '{{json .}}'` sie liefert (+ Leerzeile).
	out := `{"ID":"abc123def4567890","Names":"web","Image":"nginx:latest","State":"running","Status":"Up 3 hours","Ports":"0.0.0.0:80->80/tcp","Labels":"com.docker.compose.project=shop,foo=bar","CreatedAt":"2026-01-01 10:00:00 +0000 UTC"}
{"ID":"sha256:def456","Names":"db","Image":"postgres:16","State":"exited","Status":"Exited (0) 2 days ago","Ports":"","Labels":"","CreatedAt":"2026-01-01 09:00:00 +0000 UTC"}

`
	cs := parseDockerContainers(out)
	if len(cs) != 2 {
		t.Fatalf("erwartete 2 Container, bekam %d: %+v", len(cs), cs)
	}
	if cs[0].ContainerID != "abc123def456" || cs[0].Name != "web" || cs[0].Image != "nginx:latest" ||
		cs[0].State != "running" || cs[0].Compose != "shop" || cs[0].Ports == "" {
		t.Fatalf("Container 0 falsch geparst: %+v", cs[0])
	}
	if cs[1].State != "exited" || cs[1].Compose != "" || cs[1].ContainerID != "def456" {
		t.Fatalf("Container 1 falsch geparst: %+v", cs[1])
	}
	// Ungültige Zeilen werden übersprungen.
	if got := parseDockerContainers("kein json\n{kaputt}\n"); got != nil {
		t.Fatalf("ungültige Zeilen sollten ignoriert werden: %+v", got)
	}
}

func TestParseDockerImages(t *testing.T) {
	out := `{"Repository":"nginx","Tag":"latest","ID":"111122223333aaaa","Size":"142MB","CreatedSince":"2 weeks ago"}
{"Repository":"<none>","Tag":"<none>","ID":"444455556666","Size":"9.1MB","CreatedSince":"3 months ago"}`
	ims := parseDockerImages(out)
	if len(ims) != 2 {
		t.Fatalf("erwartete 2 Images, bekam %d", len(ims))
	}
	if ims[0].Repository != "nginx" || ims[0].Tag != "latest" || ims[0].ImageID != "111122223333" || ims[0].Size != "142MB" {
		t.Fatalf("Image 0 falsch: %+v", ims[0])
	}
}

func TestComposeProject(t *testing.T) {
	if p := composeProject("a=1,com.docker.compose.project=myproj,b=2"); p != "myproj" {
		t.Fatalf("compose = %q", p)
	}
	if p := composeProject("keine labels"); p != "" {
		t.Fatalf("erwartete leer, bekam %q", p)
	}
}
