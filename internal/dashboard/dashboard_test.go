package dashboard

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	sub, _ := fs.Sub(assets, "out")
	document, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), "The key is exchanged for a short-lived HttpOnly session cookie") {
		t.Fatal("embedded dashboard HTML is stale: cookie-backed login copy is missing")
	}
	hasActionableLoginError := false
	hasTerminalClipboard := false
	hasTerminalOutputMirror := false
	if err := fs.WalkDir(sub, "_next/static/chunks", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".js") {
			return nil
		}
		bundle, err := fs.ReadFile(sub, name)
		if err != nil {
			return err
		}
		hasActionableLoginError = hasActionableLoginError || strings.Contains(string(bundle), "sign in again with a valid API key")
		hasTerminalClipboard = hasTerminalClipboard || strings.Contains(string(bundle), "Copy selection")
		hasTerminalOutputMirror = hasTerminalOutputMirror || strings.Contains(string(bundle), "Terminal output")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasActionableLoginError || !hasTerminalClipboard || !hasTerminalOutputMirror {
		t.Fatal("embedded dashboard bundle is stale: expected actionable login errors, terminal clipboard controls, and an accessible output transcript")
	}
	assetReferences := regexp.MustCompile(`(?:src|href)="(/_next/[^\"]+)"`).FindAllSubmatch(document, -1)
	if len(assetReferences) < 5 {
		t.Fatalf("embedded dashboard HTML has only %d Next.js asset references", len(assetReferences))
	}
	for _, reference := range assetReferences {
		asset := strings.TrimPrefix(string(reference[1]), "/")
		if _, err := fs.Stat(sub, asset); err != nil {
			t.Errorf("dashboard HTML references missing asset %q: %v", asset, err)
		}
	}
	var scriptPath string
	if err := fs.WalkDir(sub, "_next/static/chunks", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".js") {
			scriptPath = "/" + name
			return fs.SkipAll
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if scriptPath == "" {
		t.Fatal("no embedded dashboard script found")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, scriptPath, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("dashboard script not served: status=%d type=%q", w.Code, w.Header().Get("Content-Type"))
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
