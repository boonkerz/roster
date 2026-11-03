package shared

import "testing"

func TestRegistryPath(t *testing.T) {
	for p, want := range map[string]bool{
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Run`:  true,
		`hkey_local_machine/Software/Foo`:                     true,
		`HKU\*\SOFTWARE\Microsoft\Windows\CurrentVersion\Run`: true,
		`C:\Windows\System32`:                                 false,
		`/etc`:                                                false,
		`HKLMX\foo`:                                           false,
		`HKLM`:                                                false, // ohne Unterschlüssel kein gültiger Pfad
	} {
		if got := IsRegistryPath(p); got != want {
			t.Errorf("%q: %v, want %v", p, got, want)
		}
	}
	if got := NormalizeRegistryPath(`hkey_local_machine/Software\Foo\`); got != `HKLM\Software\Foo` {
		t.Errorf("normalize: %q", got)
	}
	if h, s := SplitRegistryPath(`HKEY_USERS\*\Software`); h != "HKU" || s != `*\Software` {
		t.Errorf("split: %q %q", h, s)
	}
}
