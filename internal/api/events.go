package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/store"
)

func (s *server) listRecentLimitBreaches(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.store.(store.RecentLimitBreachStore)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "audit_store_unavailable"})
		return
	}
	items, err := persist.RecentLimitBreaches(r.Context(), key.ID, key.Scope == "admin", 20)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "audit_list_failed"})
		return
	}
	responses := make([]auditEventResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, auditEventResponse{ID: item.ID, SessionID: item.SessionID, KeyID: item.KeyID, Type: item.Type, Timestamp: item.Timestamp.UTC().Format(time.RFC3339Nano), Detail: json.RawMessage(item.Detail)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": responses})
}

type auditEventResponse struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	KeyID     string          `json:"key_id"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Detail    json.RawMessage `json:"detail"`
}

func (s *server) listSessionEvents(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	limit, cursor, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	persist, ok := s.store.(store.AuditEventStore)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "audit_store_unavailable"})
		return
	}
	items, hasMore, err := persist.PageAuditEvents(r.Context(), session.ID, limit, cursor)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal server error", Status: http.StatusInternalServerError, Code: "audit_list_failed"})
		return
	}
	responses := make([]auditEventResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, auditEventResponse{ID: item.ID, SessionID: item.SessionID, KeyID: item.KeyID, Type: item.Type, Timestamp: item.Timestamp.UTC().Format(time.RFC3339Nano), Detail: json.RawMessage(item.Detail)})
	}
	result := map[string]any{"items": responses}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		result["next_cursor"] = encodeListCursor(last.Timestamp, last.ID)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) listSessionEgressLog(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	limit, cursor, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	persist, ok := s.store.(store.EgressLogStore)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "egress_log_unavailable"})
		return
	}
	items, hasMore, err := persist.PageEgressEvents(r.Context(), session.ID, limit, cursor)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal server error", Status: http.StatusInternalServerError, Code: "egress_log_failed"})
		return
	}
	responses := make([]auditEventResponse, 0, len(items))
	for _, item := range items {
		detail, _ := json.Marshal(map[string]any{"host": item.Host, "port": item.Port, "resolved_ip": item.Resolved, "verdict": item.Verdict, "reason": item.Reason, "bytes_up": item.BytesUp, "bytes_down": item.BytesDown, "duration_ms": item.Duration})
		eventType := "egress.deny"
		if item.Verdict == "allow" {
			eventType = "egress.allow"
		}
		responses = append(responses, auditEventResponse{ID: item.ID, SessionID: item.SessionID, KeyID: item.KeyID, Type: eventType, Timestamp: item.Timestamp.UTC().Format(time.RFC3339Nano), Detail: detail})
	}
	result := map[string]any{"items": responses}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		result["next_cursor"] = encodeListCursor(last.Timestamp, last.ID)
	}
	writeJSON(w, http.StatusOK, result)
}
