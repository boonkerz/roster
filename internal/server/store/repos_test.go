package store_test

import (
	"context"
	"testing"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

func TestReposAndPackagesFleet(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	mk := func(host string) *model.Device {
		d := &model.Device{ID: store.NewID(), Hostname: host, OS: "ubuntu"}
		if err := st.CreateDevice(ctx, d, auth.HashToken(host)); err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b := mk("a"), mk("b")
	inv := func(sw ...shared.SoftwarePackage) shared.Inventory {
		return shared.Inventory{Hostname: "x", Software: sw, AutoUpdates: "security", KernelRunning: "6.8.0-45-generic",
			KernelInstalled: "6.8.0-47-generic", OSCodename: "jammy", OSEdition: "Server"}
	}
	if err := st.UpdateInventory(ctx, a.ID, inv(shared.SoftwarePackage{Name: "bash", Version: "5.1"}, shared.SoftwarePackage{Name: "curl", Version: "7.8"})); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateInventory(ctx, b.ID, inv(shared.SoftwarePackage{Name: "bash", Version: "5.2"})); err != nil {
		t.Fatal(err)
	}
	repos := []shared.PackageRepo{
		{ID: "sources.list:1", Name: "jammy", URL: "http://archive.ubuntu.com/ubuntu", Type: "apt", Enabled: true, GPGCheck: true, Suite: "jammy main"},
		{ID: "docker.list:1", Name: "jammy", URL: "https://download.docker.com/linux/ubuntu", Type: "apt", Enabled: true, GPGCheck: true, ThirdParty: true},
		{ID: "x.list:1", Name: "old", URL: "http://old.example", Type: "apt", Enabled: false, GPGCheck: false, ThirdParty: true},
	}
	if err := st.ReplacePackageRepos(ctx, a.ID, repos); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplacePackageRepos(ctx, a.ID, repos); err != nil { // idempotent (ersetzt)
		t.Fatal(err)
	}
	da, err := st.GetDevice(ctx, a.ID)
	if err != nil || len(da.Repos) != 3 || !da.Repos[0].ThirdParty || da.AutoUpdates != "security" || da.KernelInstalled != "6.8.0-47-generic" || da.OSCodename != "jammy" {
		t.Fatalf("GetDevice: %v %+v", err, da)
	}
	fleet, err := st.ReposFleet(ctx, store.RepoFilter{ThirdPartyOnly: true})
	if err != nil || len(fleet) != 2 || fleet[0].DeviceID != a.ID || !fleet[0].ThirdParty {
		t.Fatalf("ReposFleet third: %v %+v", err, fleet)
	}
	if rows, _ := st.ReposFleet(ctx, store.RepoFilter{NoGPGOnly: true}); len(rows) != 1 || rows[0].Name != "old" {
		t.Fatalf("nogpg: %+v", rows)
	}
	if rows, _ := st.ReposFleet(ctx, store.RepoFilter{Query: "docker"}); len(rows) != 1 {
		t.Fatalf("query: %+v", rows)
	}
	pk, err := st.PackagesFleet(ctx, nil, "", 0)
	if err != nil || len(pk) != 3 || pk[0].Name != "bash" || pk[0].Version != "5.1" || pk[0].Devices != 1 {
		t.Fatalf("PackagesFleet: %v %+v", err, pk)
	}
	if rows, _ := st.PackagesFleet(ctx, nil, "cur", 0); len(rows) != 1 || rows[0].Name != "curl" {
		t.Fatalf("PackagesFleet q: %+v", rows)
	}
	devs, err := st.PackageDevices(ctx, nil, "bash", "5.2")
	if err != nil || len(devs) != 1 || devs[0].DeviceID != b.ID {
		t.Fatalf("PackageDevices: %v %+v", err, devs)
	}
	if devs, _ := st.PackageDevices(ctx, nil, "bash", ""); len(devs) != 2 {
		t.Fatalf("PackageDevices all: %+v", devs)
	}
	if devs, _ := st.PackageDevices(ctx, map[string]bool{}, "bash", ""); len(devs) != 0 {
		t.Fatalf("scope leer: %+v", devs)
	}
}
