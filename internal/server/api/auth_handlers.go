package api

import (
	"net/http"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
)

const sessionTTL = 12 * time.Hour

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin prüft lokale Anmeldedaten und setzt ein Session-Cookie.
// (LDAP-Quelle wird in M4 ergänzt; die auth_source steht bereits im Modell.)
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		s.logAudit(r, "", req.Username, "Anmeldung fehlgeschlagen", http.StatusUnauthorized)
		s.writeErr(w, http.StatusUnauthorized, "ungültige anmeldedaten")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		s.logAudit(r, user.ID, req.Username, "Anmeldung fehlgeschlagen", http.StatusUnauthorized)
		s.writeErr(w, http.StatusUnauthorized, "ungültige anmeldedaten")
		return
	}

	// Hat der Benutzer 2FA aktiv, zuerst den zweiten Faktor verlangen (keine Session).
	if user.TOTPEnabled {
		pending := auth.GenerateToken()
		exp := time.Now().Add(5 * time.Minute)
		if err := s.store.CreateLoginChallenge(r.Context(), auth.HashToken(pending), user.ID, exp); err != nil {
			s.mapStoreErr(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"totp_required": true, "pending": pending})
		return
	}

	// Sonst Session ausstellen. Ist 2FA Pflicht und noch nicht eingerichtet, erzwingt
	// das Frontend die Einrichtung (Backend beschränkt die Session via requireEnrolled).
	s.startSession(w, r, user)
}

// startSession erstellt Session-Cookie + Datensatz und antwortet mit dem Benutzer.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *model.User) {
	token := auth.GenerateToken()
	expires := time.Now().Add(sessionTTL)
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), user.ID, expires); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	_ = s.store.UpdateLastLogin(r.Context(), user.ID, time.Now().UTC())
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	user.Require2FA = s.cfg.Require2FA
	// Effektive Rechte für die direkte Login-Antwort berechnen (kein requireUser davor).
	var customPerms []string
	if user.Role != model.RoleAdmin && user.CustomRoleID != "" {
		customPerms, _ = s.store.CustomRolePermissions(r.Context(), user.CustomRoleID)
	}
	user.Permissions = model.EffectivePermissions(user, customPerms)
	s.logAudit(r, user.ID, user.Username, "Anmeldung", http.StatusOK)
	s.writeJSON(w, http.StatusOK, user)
}

// handleLogout löscht die Session serverseitig und das Cookie clientseitig.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.store.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "abgemeldet"})
}

// handleMe gibt den angemeldeten Benutzer zurück (inkl. 2FA-Pflicht-Hinweis).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := *userFrom(r.Context())
	u.Require2FA = s.cfg.Require2FA
	s.writeJSON(w, http.StatusOK, &u)
}

type setThemeRequest struct {
	Theme string `json:"theme"`
}

// handleSetTheme speichert die Theme-Präferenz des angemeldeten Benutzers.
func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	var req setThemeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Theme != "light" && req.Theme != "dark" {
		s.writeErr(w, http.StatusBadRequest, "theme muss 'light' oder 'dark' sein")
		return
	}
	if err := s.store.UpdateUserTheme(r.Context(), userFrom(r.Context()).ID, req.Theme); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"theme": req.Theme})
}

type changePasswordRequest struct {
	Current string `json:"current_password"`
	New     string `json:"new_password"`
}

// handleChangePassword ändert das Passwort des angemeldeten Benutzers nach
// Bestätigung des aktuellen Passworts (nur lokale Konten).
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var req changePasswordRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if u.AuthSource != model.AuthLocal {
		s.writeErr(w, http.StatusBadRequest, "Passwort kann nur bei lokalen Konten geändert werden")
		return
	}
	if len(req.New) < 8 {
		s.writeErr(w, http.StatusBadRequest, "das neue Passwort muss mindestens 8 Zeichen haben")
		return
	}
	ok, err := auth.VerifyPassword(req.Current, u.PasswordHash)
	if err != nil || !ok {
		s.writeErr(w, http.StatusUnauthorized, "das aktuelle Passwort ist falsch")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "passwort konnte nicht verarbeitet werden")
		return
	}
	if err := s.store.SetUserPassword(r.Context(), u.ID, hash); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
