package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/shared/compliance"
)

// handleComplianceRules liefert den Regelkatalog (für Richtlinien-Editor und Anzeige).
func (s *Server) handleComplianceRules(w http.ResponseWriter, r *http.Request) {
	// Leere Slices statt null ausliefern – Regeln ohne Profil-/Plattform-Einschränkung
	// haben im Katalog nil-Felder.
	out := make([]compliance.Rule, len(compliance.Catalog))
	for i, rule := range compliance.Catalog {
		if rule.Profiles == nil {
			rule.Profiles = []string{}
		}
		if rule.Platforms == nil {
			rule.Platforms = []string{}
		}
		if rule.Frameworks == nil {
			rule.Frameworks = []string{}
		}
		out[i] = rule
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleComplianceFleet liefert Score und Zähler je geprüftem Gerät.
func (s *Server) handleComplianceFleet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ComplianceFleet(r.Context(), s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleComplianceByRule liefert die Pass-/Fail-Zahlen je Regel.
func (s *Server) handleComplianceByRule(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ComplianceByRule(r.Context(), s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleComplianceRuleDevices liefert die Geräte zu einer Regel (?status=fail).
func (s *Server) handleComplianceRuleDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ComplianceRuleDevices(r.Context(), s.scopeFilter(r), chi.URLParam(r, "id"), r.URL.Query().Get("status"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleDeviceCompliance liefert die Regelergebnisse eines Geräts.
func (s *Server) handleDeviceCompliance(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ComplianceFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleComplianceScan stößt eine sofortige Prüfung auf dem Gerät an.
func (s *Server) handleComplianceScan(w http.ResponseWriter, r *http.Request) {
	s.queueDeviceCmd(w, r, "compliance_scan", "Compliance-Prüfung", nil)
}
