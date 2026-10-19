package api

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/store"
)

const maxIdempotentRequestBytes = 2 << 20

type idemLock struct {
	mu   sync.Mutex
	refs int
}

func (s *server) lockIdempotency(key string) func() {
	s.idemMu.Lock()
	entry := s.idemLocks[key]
	if entry == nil {
		entry = &idemLock{}
		s.idemLocks[key] = entry
	}
	entry.refs++
	s.idemMu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.idemMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(s.idemLocks, key)
		}
		s.idemMu.Unlock()
	}
}

func (s *server) idempotencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if r.Method != http.MethodPost || key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 255 {
			problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Detail: "Idempotency-Key must be at most 255 bytes"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotentRequestBytes+1))
		if err != nil {
			problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_body_invalid"})
			return
		}
		if len(body) > maxIdempotentRequestBytes {
			problem.Write(w, problem.Problem{Title: "Request too large", Status: http.StatusRequestEntityTooLarge, Code: "request_too_large"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		authKey := sha256.Sum256([]byte(r.Header.Get("Authorization") + "\x00" + key))
		requestHash := sha256.Sum256([]byte(r.Method + "\x00" + r.URL.RequestURI() + "\x00" + string(body)))
		lockKey := string(authKey[:])
		unlock := s.lockIdempotency(lockKey)
		defer unlock()
		idemStore, ok := s.store.(store.IdempotencyStore)
		if !ok {
			problem.Write(w, problem.Problem{Title: "Service unavailable", Status: http.StatusServiceUnavailable, Code: "idempotency_unavailable"})
			return
		}
		existing, err := idemStore.GetIdempotency(r.Context(), authKey[:])
		if err == nil {
			if !bytes.Equal(existing.RequestHash, requestHash[:]) {
				problem.Write(w, problem.Problem{Title: "Unprocessable request", Status: http.StatusUnprocessableEntity, Code: "idempotency_key_reused", Detail: "this Idempotency-Key was already used with a different request"})
				return
			}
			replayIdempotencyResponse(w, existing)
			return
		}
		if err != sql.ErrNoRows {
			problem.Write(w, problem.Problem{Title: "Service unavailable", Status: http.StatusServiceUnavailable, Code: "idempotency_unavailable"})
			return
		}
		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, r)
		result := recorder.Result()
		defer result.Body.Close()
		responseBody, readErr := io.ReadAll(result.Body)
		if readErr != nil {
			problem.Write(w, problem.Problem{Title: "Internal server error", Status: http.StatusInternalServerError, Code: "response_capture_failed"})
			return
		}
		if result.StatusCode >= 200 && result.StatusCode < 300 {
			headers, marshalErr := json.Marshal(result.Header)
			if marshalErr != nil || idemStore.SaveIdempotency(r.Context(), store.IdempotencyRecord{KeyHash: authKey[:], RequestHash: requestHash[:], StatusCode: result.StatusCode, HeadersJSON: headers, Body: responseBody, CreatedAt: time.Now().UTC()}) != nil {
				problem.Write(w, problem.Problem{Title: "Service unavailable", Status: http.StatusServiceUnavailable, Code: "idempotency_unavailable"})
				return
			}
		}
		copyResponse(w, result.Header, result.StatusCode, responseBody)
	})
}

func replayIdempotencyResponse(w http.ResponseWriter, record store.IdempotencyRecord) {
	var headers http.Header
	if json.Unmarshal(record.HeadersJSON, &headers) != nil || headers == nil {
		headers = make(http.Header)
	}
	copyResponse(w, headers, record.StatusCode, record.Body)
}

func copyResponse(w http.ResponseWriter, headers http.Header, status int, body []byte) {
	for name, values := range headers {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
