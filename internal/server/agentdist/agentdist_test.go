package agentdist

import (
	"bytes"
	"debug/pe"
	"strings"
	"testing"
)

// TestWindowsAgentRunsOnServer2012 ist die Wache gegen das versehentliche Aussperren
// älterer Windows-Versionen (Windows Server 2012/2012 R2, Windows 7/8.1): Der Go-Linker
// schreibt 10.0 als Mindestversion in den PE-Kopf, tools/pepatch senkt sie im Makefile
// auf 6.1. Fehlt dieser Schritt oder zieht ein neuer Import eine Windows-10-API statisch
// herein, schlägt der Test fehl. Ohne eingebettetes Binary (kein `make agents-embed`)
// wird übersprungen.
func TestWindowsAgentRunsOnServer2012(t *testing.T) {
	data, _, ok := Read("windows-amd64")
	if !ok || len(data) == 0 {
		t.Skip("agent-windows-amd64.exe nicht eingebettet (make agents-embed)")
	}
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PE lesen: %v", err)
	}
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("kein PE32+-Header")
	}
	if oh.MajorSubsystemVersion > 6 || (oh.MajorSubsystemVersion == 6 && oh.MinorSubsystemVersion > 1) ||
		oh.MajorOperatingSystemVersion > 6 || (oh.MajorOperatingSystemVersion == 6 && oh.MinorOperatingSystemVersion > 1) {
		t.Fatalf("Mindest-Windows-Version im PE-Kopf ist %d.%d/%d.%d – muss 6.1 sein (tools/pepatch im Makefile fehlt?); Windows Server 2012 lehnt die Datei sonst als „keine gültige Win32-Anwendung“ ab",
			oh.MajorOperatingSystemVersion, oh.MinorOperatingSystemVersion, oh.MajorSubsystemVersion, oh.MinorSubsystemVersion)
	}
	// Statische Importe: Go bindet nur kernel32 – alles andere (api-ms-win-*, ucrtbase,
	// user32 …) wäre neu und müsste auf Windows 8/Server 2012 geprüft werden.
	syms, err := f.ImportedSymbols()
	if err != nil {
		t.Fatalf("Importe lesen: %v", err)
	}
	win10Only := map[string]bool{
		"SetThreadDescription": true, "GetThreadDescription": true, "VirtualAlloc2": true, "MapViewOfFile3": true,
		"VirtualAllocFromApp": true, "DiscardVirtualMemory": true, "OfferVirtualMemory": true, "GetTempPath2W": true,
		"QueryUnbiasedInterruptTimePrecise": true, "GetSystemCpuSetInformation": true, "SetProcessDynamicEHContinuationTargets": true,
	}
	for _, s := range syms {
		name, lib, _ := strings.Cut(s, ":")
		if !strings.EqualFold(lib, "kernel32.dll") {
			t.Errorf("statischer Import aus %s (%s) – auf Windows Server 2012 prüfen", lib, name)
		}
		if win10Only[name] {
			t.Errorf("statischer Import %s gibt es erst ab Windows 10", name)
		}
	}
}
