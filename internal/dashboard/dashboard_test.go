package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedDashboardAndAPIFallback(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	handler := Wrap(api)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "cellbox") {
		t.Fatalf("dashboard root not served: status=%d body=%q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("API fallback status=%d, want 202", w.Code)
	}
}

func TestDashboardLoginUsesHttpOnlyCookieAndCSRF(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cbx_test-secret" {
			t.Errorf("API key was not forwarded")
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[]}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewTLSServer(Wrap(api))
	defer server.Close()
	client := server.Client()
	login, err := client.Post(server.URL+"/v1/auth/session", "application/json", strings.NewReader(`{"api_key":"cbx_test-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", login.StatusCode)
	}
	var response struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.CSRF == "" {
		t.Fatal("login returned no CSRF token")
	}
	var session, csrf *http.Cookie
	for _, cookie := range login.Cookies() {
		if cookie.Name == sessionCookie {
			session = cookie
		}
		if cookie.Name == csrfCookie {
			csrf = cookie
		}
	}
	if session == nil || csrf == nil {
		t.Fatalf("login cookies missing: %#v", login.Cookies())
	}
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie flags: %#v", session)
	}
	if csrf.HttpOnly || !csrf.Secure || csrf.SameSite != http.SameSiteStrictMode || csrf.Value != response.CSRF {
		t.Fatalf("CSRF cookie flags/value: %#v", csrf)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/sessions", strings.NewReader(`{}`))
	request.AddCookie(session)
	request.AddCookie(csrf)
	withoutHeader, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if withoutHeader.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF header status %d", withoutHeader.StatusCode)
	}
	withoutHeader.Body.Close()

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/sessions", strings.NewReader(`{}`))
	request.AddCookie(session)
	request.AddCookie(csrf)
	request.Header.Set("X-CSRF-Token", response.CSRF)
	created, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if created.StatusCode != http.StatusNoContent {
		t.Fatalf("cookie-authenticated API status %d", created.StatusCode)
	}
	created.Body.Close()

	logout, _ := http.NewRequest(http.MethodDelete, server.URL+"/v1/auth/session", nil)
	logout.AddCookie(session)
	logout.AddCookie(csrf)
	logout.Header.Set("X-CSRF-Token", response.CSRF)
	loggedOut, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	if loggedOut.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status %d", loggedOut.StatusCode)
	}
	loggedOut.Body.Close()
}
