package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/vncdist"
)

// handleVNCBundle liefert das native VNC-Server-Bundle (ZIP) einer Plattform an den
// Agent (requireAgent). Der SHA-256 steht im Header X-VNC-SHA256 für den Cache-Abgleich.
func (s *Server) handleVNCBundle(w http.ResponseWriter, r *http.Request) {
	plat := chi.URLParam(r, "platform")
	data, sha, ok := vncdist.Read(plat)
	if !ok {
		s.writeErr(w, http.StatusNotFound, "kein VNC-Bundle für diese Plattform")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-VNC-SHA256", sha)
	w.Header().Set("Content-Disposition", `attachment; filename="vnc.zip"`)
	_, _ = w.Write(data)
}
