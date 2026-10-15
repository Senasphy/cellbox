package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/store"
)

type keyPersistence interface {
	CreateKey(context.Context, store.KeyRecord) error
	ListKeys(context.Context) ([]store.KeyRecord, error)
	RevokeKey(context.Context, string) error
}

type createKeyRequest struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type keyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	Secret     string     `json:"secret,omitempty"`
}

func (s *server) createKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	persist, ok := s.keyStore(w)
	if !ok {
		return
	}
	var req createKeyRequest
	if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 128 || strings.ContainsRune(req.Name, 0) {
		problem.Write(w, problem.Problem{Title: "Invalid key", Status: http.StatusUnprocessableEntity, Code: "key_invalid", Detail: "name must contain 1 to 128 characters"})
		return
	}
	if req.Scope != "admin" && req.Scope != "session" {
		problem.Write(w, problem.Problem{Title: "Invalid key scope", Status: http.StatusUnprocessableEntity, Code: "key_scope_invalid", Detail: "scope must be admin or session"})
		return
	}
	keyID, err := id.New("key")
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "id_generation_failed"})
		return
	}
	key, err := auth.Generate(keyID, req.Name, req.Scope)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "key_generation_failed"})
		return
	}
	record := store.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}
	if err := persist.CreateKey(r.Context(), record); err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "key_create_failed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toKeyResponse(record, key.Secret))
}

func (s *server) listKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	persist, ok := s.keyStore(w)
	if !ok {
		return
	}
	keys, err := persist.ListKeys(r.Context())
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "key_list_failed"})
		return
	}
	items := make([]keyResponse, 0, len(keys))
	for _, key := range keys {
		items = append(items, toKeyResponse(key, ""))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	persist, ok := s.keyStore(w)
	if !ok {
		return
	}
	err := persist.RevokeKey(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "key_not_found"})
		return
	}
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "key_revoke_failed"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) requireAdmin(w http.ResponseWriter, r *http.Request) (store.KeyRecord, bool) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return store.KeyRecord{}, false
	}
	if key.Scope != "admin" {
		problem.Write(w, problem.Problem{Title: "Forbidden", Status: http.StatusForbidden, Code: "insufficient_scope"})
		return store.KeyRecord{}, false
	}
	return key, true
}

func (s *server) keyStore(w http.ResponseWriter) (keyPersistence, bool) {
	persist, ok := s.store.(keyPersistence)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "store_unavailable"})
		return nil, false
	}
	return persist, true
}

func toKeyResponse(key store.KeyRecord, secret string) keyResponse {
	return keyResponse{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Scope: key.Scope, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt, RevokedAt: key.RevokedAt, Secret: secret}
}
