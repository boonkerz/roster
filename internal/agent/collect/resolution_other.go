//go:build !windows

package collect

// Auflösungssteuerung ist derzeit nur unter Windows implementiert (Win32).

func ListResolutions() string {
	return `{"error":"Auflösungssteuerung nur unter Windows verfügbar","modes":[]}`
}

func SetResolution(w, h int) (int, string) {
	return -1, "Auflösungssteuerung nur unter Windows unterstützt"
}
