package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
)

// Software-Verteilung: ein Katalog verteilbarer Pakete (Kennung je Paketmanager)
// plus eine Bulk-Aktion, die die Installation auf allen Geräten eines Ziels einreiht.

func (s *Server) handleListPackages(w http.ResponseWriter, r *http.Request) {
	pkgs, err := s.store.ListDeployPackages(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if pkgs == nil {
		pkgs = []model.DeployPackage{}
	}
	s.writeJSON(w, http.StatusOK, pkgs)
}

func (s *Server) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	var p model.DeployPackage
	if !s.decodeJSON(w, r, &p) {
		return
	}
	if p.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "Name fehlt")
		return
	}
	p.ID = store.NewID()
	if err := s.store.CreateDeployPackage(r.Context(), &p); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePackage(w http.ResponseWriter, r *http.Request) {
	var p model.DeployPackage
	if !s.decodeJSON(w, r, &p) {
		return
	}
	p.ID = chi.URLParam(r, "id")
	if err := s.store.UpdateDeployPackage(r.Context(), &p); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePackage(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDeployPackage(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBulkInstallPackage reiht die Installation eines Katalog-Pakets für alle
// Geräte eines Ziels ein.
func (s *Server) handleBulkInstallPackage(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	pkg, err := s.store.GetDeployPackage(r.Context(), req.PackageID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	ids, ok := s.resolveBulkDevices(w, r, req)
	if !ok {
		return
	}
	payload := map[string]any{
		"winget": pkg.Winget, "choco": pkg.Choco, "apt": pkg.Apt, "dnf": pkg.Dnf, "brew": pkg.Brew,
	}
	label := "Installieren: " + pkg.Name
	queued := 0
	for _, dev := range ids {
		if _, err := s.queueCommand(r.Context(), dev, "install_package", label, payload); err == nil {
			queued++
		}
	}
	s.writeJSON(w, http.StatusCreated, map[string]int{"queued": queued})
}
