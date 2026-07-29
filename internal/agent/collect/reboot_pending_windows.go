//go:build windows

package collect

import "golang.org/x/sys/windows/registry"

// RebootPending prüft die üblichen Windows-Indikatoren für einen ausstehenden
// Neustart (Component Based Servicing, Windows Update, PendingFileRenameOperations).
func RebootPending() (pending, known bool) {
	// Schlüssel, deren bloße Existenz einen ausstehenden Neustart signalisiert.
	for _, path := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
	} {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE); err == nil {
			k.Close()
			return true, true
		}
	}
	// Ausstehende Datei-Umbenennungen (Session Manager).
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetStringsValue("PendingFileRenameOperations"); err == nil && len(v) > 0 {
			return true, true
		}
	}
	return false, true
}
