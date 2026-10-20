package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
)

func (s *server) imageManager(w http.ResponseWriter, r *http.Request) (runtime.ImageManager, bool) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return nil, false
	}
	manager, ok := s.backend.(runtime.ImageManager)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unsupported", Status: http.StatusNotImplemented, Code: "runtime_unsupported", Detail: "runtime does not support image management"})
		return nil, false
	}
	return manager, true
}

func (s *server) listImages(w http.ResponseWriter, r *http.Request) {
	m, ok := s.imageManager(w, r)
	if !ok {
		return
	}
	images, err := m.ListImages(r.Context())
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Image listing failed", Status: http.StatusBadGateway, Code: "image_list_failed", Detail: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"images": images})
}

func (s *server) pullImage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if s.backend == nil {
		problem.Write(w, problem.Problem{Title: "Runtime unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
		return
	}
	var req struct {
		Reference string `json:"reference"`
	}
	if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	req.Reference = strings.TrimSpace(req.Reference)
	if req.Reference == "" || len(req.Reference) > 512 {
		problem.Write(w, problem.Problem{Title: "Invalid image", Status: http.StatusUnprocessableEntity, Code: "image_reference_invalid"})
		return
	}
	image, err := s.backend.EnsureImage(r.Context(), req.Reference)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Image pull failed", Status: http.StatusBadGateway, Code: "image_pull_failed", Detail: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(image)
}

func (s *server) removeImage(w http.ResponseWriter, r *http.Request) {
	m, ok := s.imageManager(w, r)
	if !ok {
		return
	}
	reference := strings.TrimSpace(r.URL.Query().Get("reference"))
	if reference == "" || len(reference) > 512 {
		problem.Write(w, problem.Problem{Title: "Invalid image", Status: http.StatusUnprocessableEntity, Code: "image_reference_invalid"})
		return
	}
	if err := m.RemoveImage(r.Context(), reference); err != nil {
		problem.Write(w, problem.Problem{Title: "Image removal failed", Status: http.StatusConflict, Code: "image_remove_failed", Detail: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
