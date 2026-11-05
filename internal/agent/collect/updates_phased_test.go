//go:build linux

package collect

import (
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestParseAptPhased(t *testing.T) {
	out := "Reading package lists...\nBuilding dependency tree...\nCalculating upgrade...\nThe following upgrades have been deferred due to phasing:\n  base-files motd-news-config vim vim-common\n  xxd\nThe following packages will be upgraded:\n  curl libcurl4\n2 upgraded, 0 newly installed, 0 to remove and 5 not upgraded.\n"
	ph := parseAptPhased(out)
	if len(ph) != 5 || !ph["vim"] || !ph["xxd"] || ph["curl"] {
		t.Fatalf("phased: %v", ph)
	}
	items := []shared.UpdateItem{{Name: "vim"}, {Name: "curl"}, {Name: "libc6:amd64"}}
	markPhased(items, map[string]bool{"vim": true, "libc6": true})
	if !items[0].Phased || items[1].Phased || !items[2].Phased {
		t.Fatalf("mark: %+v", items)
	}
	if len(parseAptPhased("0 upgraded, 0 newly installed.\n")) != 0 {
		t.Fatal("ohne Block leer")
	}
}
