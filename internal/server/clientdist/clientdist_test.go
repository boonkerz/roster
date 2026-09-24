package clientdist

import (
	"testing"
	"testing/fstest"
)

func testSet() Set {
	return Set{
		FS: fstest.MapFS{
			"bin/tool-linux-amd64":       {Data: []byte("linux!")},
			"bin/tool-windows-amd64.zip": {Data: []byte("zip")},
		},
		Files: map[string]string{
			"linux-amd64":   "bin/tool-linux-amd64",
			"windows-amd64": "bin/tool-windows-amd64.zip",
			"darwin-arm64":  "bin/tool-darwin-arm64.zip", // nicht gebaut
		},
		Names: map[string]string{
			"linux-amd64":   "tool",
			"windows-amd64": "tool-windows.zip",
			"darwin-arm64":  "tool-macos.zip",
		},
	}
}

func TestAvailableSkipsMissingBuilds(t *testing.T) {
	got := testSet().Available()
	want := []string{"linux-amd64", "windows-amd64"}
	if len(got) != len(want) {
		t.Fatalf("Available() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Available() = %v, want %v", got, want)
		}
	}
}

func TestReadAndList(t *testing.T) {
	s := testSet()
	data, name, ok := s.Read("linux-amd64")
	if !ok || name != "tool" || string(data) != "linux!" {
		t.Fatalf("Read(linux-amd64) = %q, %q, %v", data, name, ok)
	}
	if _, _, ok := s.Read("darwin-arm64"); ok {
		t.Fatal("Read(darwin-arm64) should fail for a platform that was not built")
	}
	if _, _, ok := s.Read("plan9-mips"); ok {
		t.Fatal("Read(unknown) should fail")
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List() = %+v, want 2 entries", list)
	}
	if list[1].Platform != "windows-amd64" || list[1].Filename != "tool-windows.zip" || list[1].Size != 3 {
		t.Fatalf("List()[1] = %+v", list[1])
	}
	if (Set{FS: fstest.MapFS{}}).List() == nil {
		t.Fatal("List() on an empty set must return an empty slice, not nil")
	}
}
