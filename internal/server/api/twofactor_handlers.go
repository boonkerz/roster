package api

import (
	"net/http"
	"strings"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
)

const issuerName = "Roster"

type totpLoginRequest struct {
	Pending string `json:"pending"`
	Code    string `json:"code"`
}

// handleLoginTOTP schließt den Login mit dem zweiten Faktor ab (TOTP oder Backup-Code).
func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req totpLoginRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	ipKey := "ip:" + s.clientIP(r)
	if s.tooMany(w, ipKey) {
		return
	}
	userID, err := s.store.ConsumeLoginChallenge(r.Context(), auth.HashToken(req.Pending))
	if err != nil {
		s.writeErr(w, http.StatusUnauthorized, "anmeldung abgelaufen – bitte erneut anmelden")
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil {
		s.writeErr(w, http.StatusUnauthorized, "ungültige anmeldedaten")
		return
	}
	if !s.verifySecondFactor(r, user, req.Code) {
		s.limiter.fail(ipKey)
		s.limiter.fail("user:" + strings.ToLower(user.Username))
		s.logAudit(r, user.ID, user.Username, "Anmeldung fehlgeschlagen (2FA-Code)", http.StatusUnauthorized)
		s.writeErr(w, http.StatusUnauthorized, "code ungültig")
		return
	}
	s.startSession(w, r, user)
}

// verifySecondFactor akzeptiert einen gültigen TOTP-Code oder einen ungenutzten Backup-Code.
func (s *Server) verifySecondFactor(r *http.Request, user *model.User, code string) bool {
	code = strings.TrimSpace(code)
	if auth.VerifyTOTP(user.TOTPSecret, code) {
		return true
	}
	used, err := s.store.ConsumeRecoveryCode(r.Context(), user.ID, auth.HashToken(strings.ToLower(code)))
	return err == nil && used
}

// handle2FASetup erzeugt ein neues (noch nicht aktives) Secret und liefert die otpauth-URL.
func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	if user.TOTPEnabled {
		s.writeErr(w, http.StatusBadRequest, "2FA ist bereits aktiv")
		return
	}
	secret := auth.GenerateTOTPSecret()
	if err := s.store.SetUserTOTP(r.Context(), user.ID, secret, false); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{
		"secret":      secret,
		"otpauth_url": auth.OTPAuthURL(issuerName, user.Username, secret),
	})
}

type codeRequest struct {
	Code string `json:"code"`
}

// handle2FAEnable aktiviert 2FA nach Bestätigung eines Codes und liefert die Backup-Codes.
func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if user.TOTPEnabled {
		s.writeErr(w, http.StatusBadRequest, "2FA ist bereits aktiv")
		return
	}
	if user.TOTPSecret == "" || !auth.VerifyTOTP(user.TOTPSecret, req.Code) {
		s.writeErr(w, http.StatusBadRequest, "code ungültig – bitte erneut versuchen")
		return
	}
	if err := s.store.SetUserTOTP(r.Context(), user.ID, user.TOTPSecret, true); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	codes := s.newRecoveryCodes(r, user.ID)
	s.writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// handle2FADisable schaltet 2FA nach Bestätigung (TOTP oder Backup-Code) wieder ab.
func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if !user.TOTPEnabled || !s.verifySecondFactor(r, user, req.Code) {
		s.writeErr(w, http.StatusBadRequest, "code ungültig")
		return
	}
	if err := s.store.ClearUserTOTP(r.Context(), user.ID); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deaktiviert"})
}

// handle2FARecovery erzeugt neue Backup-Codes (alte werden ungültig) nach Bestätigung.
func (s *Server) handle2FARecovery(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if !user.TOTPEnabled || !s.verifySecondFactor(r, user, req.Code) {
		s.writeErr(w, http.StatusBadRequest, "code ungültig")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": s.newRecoveryCodes(r, user.ID)})
}

func (s *Server) newRecoveryCodes(r *http.Request, userID string) []string {
	codes := auth.GenerateRecoveryCodes(10)
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = auth.HashToken(c)
	}
	_ = s.store.ReplaceRecoveryCodes(r.Context(), userID, hashes)
	return codes
}
