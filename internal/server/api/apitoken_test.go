package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// TestRequireUserAcceptsBearerAPIToken prüft, dass requireUser neben dem Session-
// Cookie auch ein gültiges User-API-Token akzeptiert und ungültige/fehlende Tokens
// mit 401 abweist – die Grundlage für den Zugriff der nativen App.
func TestRequireUserAcceptsBearerAPIToken(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatalf("store öffnen: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	u := &model.User{ID: store.NewID(), Username: "dave", Role: model.RoleAdmin, AuthSource: model.AuthLocal, PasswordHash: "x"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	plain := auth.GenerateToken()
	if err := st.CreateUserAPIToken(ctx, &model.UserAPIToken{ID: store.NewID(), UserID: u.ID, Label: "app"}, auth.HashToken(plain)); err != nil {
		t.Fatalf("CreateUserAPIToken: %v", err)
	}

	s := &Server{store: st, cfg: config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var gotUser string
	h := s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if uu := userFrom(r.Context()); uu != nil {
			gotUser = uu.Username
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Gültiges Bearer-Token → 200 + Benutzer im Kontext.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotUser != "dave" {
		t.Fatalf("gültiges Token: code=%d user=%q", rec.Code, gotUser)
	}

	// Ungültiges Token → 401.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer nichtgueltig")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("ungültiges Token: code=%d, erwartet 401", rec.Code)
	}

	// Ohne jegliche Anmeldung → 401.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("ohne Auth: code=%d, erwartet 401", rec.Code)
	}
}
