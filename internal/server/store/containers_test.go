package store_test

import (
	"context"
	"testing"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

func TestPruneImageScansUnusedImages(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := &model.Device{ID: "dev1", Hostname: "web-01"}
	if err := st.CreateDevice(ctx, d, "hash"); err != nil {
		t.Fatal(err)
	}
	st.ReplaceDockerImages(ctx, "dev1", []shared.DockerImage{{Repository: "nginxproxy/acme-companion", Tag: "latest", ImageID: "new111"}, {Repository: "jrcs/letsencrypt-nginx-proxy-companion", Tag: "latest", ImageID: "old222"}})
	st.ReplaceDockerContainers(ctx, "dev1", []shared.DockerContainer{{ContainerID: "c1", Name: "le", Image: "nginxproxy/acme-companion", State: "running"}})
	st.UpsertImageScans(ctx, "dev1", []shared.ImageScan{
		{ImageID: "new111", Image: "nginxproxy/acme-companion", OS: "alpine", Packages: []shared.ImagePackage{{Name: "a", Version: "1"}}},
		{ImageID: "old222", Image: "jrcs/letsencrypt-nginx-proxy-companion", OS: "alpine", Packages: []shared.ImagePackage{{Name: "a", Version: "1"}}},
	})
	if err := st.PruneImageScans(ctx, "dev1"); err != nil {
		t.Fatal(err)
	}
	scans, _ := st.ImageScansFor(ctx, "dev1")
	if len(scans) != 1 || scans[0].ImageID != "new111" {
		t.Fatalf("altes Image (lokal noch vorhanden, ohne Container) muss weg: %+v", scans)
	}
	// Docker ganz weg: keine Container, keine Images -> alles weg.
	st.ReplaceDockerImages(ctx, "dev1", nil)
	st.ReplaceDockerContainers(ctx, "dev1", nil)
	st.PruneImageScans(ctx, "dev1")
	if scans, _ := st.ImageScansFor(ctx, "dev1"); len(scans) != 0 {
		t.Fatalf("ohne Docker: %+v", scans)
	}
}
