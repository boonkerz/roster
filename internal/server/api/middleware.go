package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
)

const sessionCookie = "roster_session"

// requireUser verlangt eine gültige Session und legt den Benutzer in den Kontext.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			s.writeErr(w, http.StatusUnauthorized, "nicht angemeldet")
			return
		}
		user, err := s.store.UserBySession(r.Context(), auth.HashToken(c.Value))
		if err != nil {
			s.writeErr(w, http.StatusUnauthorized, "session ungültig oder abgelaufen")
			return
		}
		// Effektive Rechte berechnen (Admin = alle; sonst Custom-Rolle oder Default).
		var customPerms []string
		if user.Role != model.RoleAdmin && user.CustomRoleID != "" {
			customPerms, _ = s.store.CustomRolePermissions(r.Context(), user.CustomRoleID)
		}
		user.Permissions = model.EffectivePermissions(user, customPerms)
		ctx := context.WithValue(r.Context(), ctxUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireEnrolled blockiert (bei 2FA-Pflicht) alle Funktionen außer der 2FA-Einrichtung,
// solange der Benutzer noch keinen zweiten Faktor aktiviert hat.
func (s *Server) requireEnrolled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		if s.cfg.Require2FA && (u == nil || !u.TOTPEnabled) {
			s.writeErr(w, http.StatusForbidden, "zwei-faktor-einrichtung erforderlich")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin verlangt zusätzlich die Rolle admin (nach requireUser).
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFrom(r.Context()); u == nil || u.Role != model.RoleAdmin {
			s.writeErr(w, http.StatusForbidden, "adminrechte erforderlich")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireTech erlaubt Geräte-Bedienung für Techniker und Admins (nach requireUser).
func (s *Server) requireTech(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFrom(r.Context()); u == nil || !u.Role.CanOperate() {
			s.writeErr(w, http.StatusForbidden, "bedienrechte (Techniker/Admin) erforderlich")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requirePerm verlangt ein bestimmtes Recht aus den effektiven Rechten des Benutzers
// (nach requireUser). Admins haben implizit alle Rechte.
func (s *Server) requirePerm(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := userFrom(r.Context())
			if u == nil || !model.HasPermission(u.Permissions, perm) {
				s.writeErr(w, http.StatusForbidden, "keine Berechtigung für diesen Bereich")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requirePermAny verlangt mindestens eines der genannten Rechte (nach requireUser).
func (s *Server) requirePermAny(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := userFrom(r.Context())
			if u != nil {
				for _, p := range perms {
					if model.HasPermission(u.Permissions, p) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			s.writeErr(w, http.StatusForbidden, "keine Berechtigung für diesen Bereich")
		})
	}
}

// requireAgent verlangt ein gültiges, nicht widerrufenes Agent-Token.
func (s *Server) requireAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			s.writeErr(w, http.StatusUnauthorized, "agent-token fehlt")
			return
		}
		device, err := s.store.DeviceByTokenHash(r.Context(), auth.HashToken(token))
		if err != nil || device.Revoked {
			s.writeErr(w, http.StatusUnauthorized, "agent-token ungültig oder widerrufen")
			return
		}
		ctx := context.WithValue(r.Context(), ctxDevice, device)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func userFrom(ctx context.Context) *model.User {
	u, _ := ctx.Value(ctxUser).(*model.User)
	return u
}

func deviceFrom(ctx context.Context) *model.Device {
	d, _ := ctx.Value(ctxDevice).(*model.Device)
	return d
}
