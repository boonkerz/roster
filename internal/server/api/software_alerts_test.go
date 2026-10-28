package api

import (
	"testing"

	"github.com/boonkerz/roster/internal/server/model"
)

func TestValidateNotifications(t *testing.T) {
	ok, err := validateNotifications(model.PolicyNotifications{
		SoftwareAlerts:     "on",
		SoftwareAlertKinds: []string{"added", " added ", "updated"},
		SoftwareIgnore:     []string{" Microsoft Edge* ", "", "lib*"},
	})
	if err != nil {
		t.Fatalf("gültige Eingabe abgelehnt: %v", err)
	}
	if len(ok.SoftwareAlertKinds) != 2 || len(ok.SoftwareIgnore) != 2 || ok.SoftwareIgnore[0] != "Microsoft Edge*" {
		t.Fatalf("Normalisierung falsch: %+v", ok)
	}
	bad := []model.PolicyNotifications{
		{SoftwareAlerts: "maybe"},
		{SoftwareAlertKinds: []string{"deleted"}},
		{SoftwareIgnore: []string{"[abc"}},
	}
	for i, b := range bad {
		if _, err := validateNotifications(b); err == nil {
			t.Fatalf("Fall %d sollte abgelehnt werden: %+v", i, b)
		}
	}
	// Leere Eingabe ist gültig und liefert leere Slices (kein null im JSON).
	n, err := validateNotifications(model.PolicyNotifications{})
	if err != nil || n.SoftwareAlertKinds == nil || n.SoftwareIgnore == nil {
		t.Fatalf("leere Eingabe: %v %+v", err, n)
	}
}
