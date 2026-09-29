package collect

import (
	"strings"
	"testing"
)

func TestPruneReport(t *testing.T) {
	entries := []pruneEntry{
		{VolID: "pi:backup/vzdump-qemu-101-2026_09_11-01_00_02.vma.zst", Mark: "keep", VMID: 101, CTime: 1757552402},
		{VolID: "pi:backup/vzdump-qemu-101-2026_09_10-01_00_02.vma.zst", Mark: "remove", VMID: 101, CTime: 1757466002},
		{VolID: "pi:backup/vzdump-lxc-100-2026_09_09-01_00_02.tar.zst", Mark: "remove", VMID: 100, CTime: 1757379602},
		{VolID: "pi:backup/vzdump-lxc-100-2026_09_11-01_00_02.tar.zst", Mark: "protected", VMID: 100, CTime: 1757552402},
	}
	out := pruneReport(entries, nil, false)
	lines := strings.Split(out, "\n")
	if lines[0] != "2 Archive entfernt, 2 bleiben" {
		t.Fatalf("zusammenfassung: %q", lines[0])
	}
	// Sortiert nach VMID, dann Zeit; Markierung lesbar.
	if !strings.HasPrefix(lines[1], "entfernt") || !strings.Contains(lines[1], "lxc-100-2026_09_09") {
		t.Fatalf("zeile 1: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "geschützt") {
		t.Fatalf("zeile 2: %q", lines[2])
	}
	dry := pruneReport(entries, []string{"102 auf pi: kaputt"}, true)
	if !strings.HasPrefix(dry, "Trockenlauf: 2 Archive würden entfernt, 2 bleiben – 1 Fehler") {
		t.Fatalf("trockenlauf: %q", strings.SplitN(dry, "\n", 2)[0])
	}
	if !strings.Contains(dry, "würde entfernt") || !strings.HasSuffix(dry, "FEHLER: 102 auf pi: kaputt") {
		t.Fatalf("trockenlauf-zeilen: %q", dry)
	}
}

func TestPruneOption(t *testing.T) {
	if got := pruneOption(1); got != "keep-last=1" {
		t.Fatalf("pruneOption = %q", got)
	}
}

func TestProxmoxPruneRejectsMissingKeep(t *testing.T) {
	if !ProxmoxAvailable() {
		t.Skip("kein pvesh")
	}
	if exit, msg := ProxmoxPrune(t.Context(), PruneSpec{KeepLast: 0}, nil); exit == 0 || !strings.Contains(msg, "keep_last") {
		t.Fatalf("keep_last=0 muss abgewiesen werden: %d %q", exit, msg)
	}
}
