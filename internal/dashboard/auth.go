package dashboard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "cellbox_session"
	csrfCookie    = "cellbox_csrf"
	sessionTTL    = 12 * time.Hour
	maxSessions   = 4096
)

type browserSession struct {
	apiKey  string
	csrf    string
	expires time.Time
}

type cookieAuth struct {
	api      http.Handler
	mu       sync.Mutex
	sessions map[string]browserSession
}

func newCookieAuth(api http.Handler) *cookieAuth {
	return &cookieAuth{api: api, sessions: make(map[string]browserSession)}
}

func (h *cookieAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/auth/session" {
		switch r.Method {
		case http.MethodPost:
			h.login(w, r)
		case http.MethodDelete:
			h.logout(w, r)
		default:
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	sessionCookieValue, err := r.Cookie(sessionCookie)
	if err == nil {
		h.mu.Lock()
		session, ok := h.sessions[sessionCookieValue.Value]
		if ok && time.Now().After(session.expires) {
			delete(h.sessions, sessionCookieValue.Value)
			ok = false
		}
		h.mu.Unlock()
		if !ok {
			h.clearCookies(w, r)
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				clone := r.Clone(r.Context())
				clone.Header = r.Header.Clone()
				clone.Header.Del("Cookie")
				h.api.ServeHTTP(w, clone)
				return
			}
			h.writeProblem(w, http.StatusUnauthorized, "unauthorized", "Dashboard session expired")
			return
		}
		if isMutation(r.Method) && !h.validCSRF(r, session.csrf) {
			h.writeProblem(w, http.StatusForbidden, "csrf_invalid", "A valid X-CSRF-Token is required for this request")
			return
		}
		clone := r.Clone(r.Context())
		clone.Header = r.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+session.apiKey)
		h.api.ServeHTTP(w, clone)
		return
	}
	h.api.ServeHTTP(w, r)
}

func (h *cookieAuth) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" && !sameOrigin(r) {
		h.writeProblem(w, http.StatusForbidden, "origin_invalid", "Dashboard login must be same-origin")
		return
	}
	var request struct {
		APIKey string `json:"api_key"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.APIKey) == "" {
		h.writeProblem(w, http.StatusBadRequest, "request_invalid", "api_key is required")
		return
	}
	probe := httptestRequest(http.MethodGet, "/v1/sessions?limit=1", "Bearer "+request.APIKey)
	recorder := &responseCapture{header: make(http.Header)}
	h.api.ServeHTTP(recorder, probe)
	if recorder.status < 200 || recorder.status >= 300 {
		h.writeProblem(w, http.StatusUnauthorized, "unauthorized", "API key is invalid or cannot access sessions")
		return
	}
	token, err := randomToken()
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "session_failed", "Could not create dashboard session")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "session_failed", "Could not create dashboard session")
		return
	}
	now := time.Now()
	h.mu.Lock()
	for value, current := range h.sessions {
		if now.After(current.expires) {
			delete(h.sessions, value)
		}
	}
	if len(h.sessions) >= maxSessions {
		h.mu.Unlock()
		h.writeProblem(w, http.StatusServiceUnavailable, "session_capacity", "Dashboard session capacity is full")
		return
	}
	h.sessions[token] = browserSession{apiKey: request.APIKey, csrf: csrf, expires: now.Add(sessionTTL)}
	h.mu.Unlock()
	h.setCookies(w, r, token, csrf, sessionTTL)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"csrf_token": csrf})
}

func (h *cookieAuth) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		h.mu.Lock()
		session, ok := h.sessions[cookie.Value]
		h.mu.Unlock()
		if !ok || !h.validCSRF(r, session.csrf) {
			h.writeProblem(w, http.StatusForbidden, "csrf_invalid", "A valid X-CSRF-Token is required for logout")
			return
		}
		h.mu.Lock()
		delete(h.sessions, cookie.Value)
		h.mu.Unlock()
	}
	h.clearCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *cookieAuth) validCSRF(r *http.Request, expected string) bool {
	cookie, err := r.Cookie(csrfCookie)
	if err != nil {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	return len(header) == len(expected) && len(cookie.Value) == len(expected) &&
		subtle.ConstantTimeCompare([]byte(expected), []byte(header)) == 1 &&
		subtle.ConstantTimeCompare([]byte(expected), []byte(cookie.Value)) == 1
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (h *cookieAuth) setCookies(w http.ResponseWriter, r *http.Request, token, csrf string, maxAge time.Duration) {
	expires := time.Now().Add(maxAge)
	secure := isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, MaxAge: int(maxAge.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: csrf, Path: "/", Expires: expires, MaxAge: int(maxAge.Seconds()), Secure: secure, SameSite: http.SameSiteStrictMode})
}

func (h *cookieAuth) clearCookies(w http.ResponseWriter, r *http.Request) {
	secure := isSecureRequest(r)
	for _, name := range []string{sessionCookie, csrfCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: name == sessionCookie, Secure: secure, SameSite: http.SameSiteStrictMode})
	}
}

func (h *cookieAuth) writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+r.Host || (r.TLS != nil && origin == "https://"+r.Host)
}

type responseCapture struct {
	header http.Header
	status int
	body   []byte
}

func (w *responseCapture) Header() http.Header    { return w.header }
func (w *responseCapture) WriteHeader(status int) { w.status = status }
func (w *responseCapture) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, body...)
	return len(body), nil
}

func httptestRequest(method, path, authorization string) *http.Request {
	r, _ := http.NewRequest(method, path, nil)
	r.Header.Set("Authorization", authorization)
	return r
}
