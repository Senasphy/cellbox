package api

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

func TestOpenAPIResponses(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join(filepath.Dir(source), "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}

	db, err := sqlite.Open(filepath.Join(t.TempDir(), "contract.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := auth.Generate("key_contract", "contract", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateKey(context.Background(), sqlite.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithConfig(db, &fakeRuntime{}, "fake", config.Defaults())

	check := func(method, target, body string, authKey bool) *httptest.ResponseRecorder {
		t.Helper()
		bodyBytes := []byte(body)
		req := httptest.NewRequest(method, target, bytes.NewReader(bodyBytes))
		req.Host = "127.0.0.1:8080"
		if body != "" {
			contentType := "application/json"
			if method == http.MethodPut && strings.Contains(target, "/archive?") {
				contentType = "application/x-tar"
			} else if method == http.MethodPut && strings.Contains(target, "/files?") {
				contentType = "application/octet-stream"
			}
			req.Header.Set("Content-Type", contentType)
		}
		if authKey {
			req.Header.Set("Authorization", "Bearer "+key.Secret)
		}
		if strings.HasSuffix(target, "/stream") && method == http.MethodGet {
			streamCtx, cancel := context.WithCancel(req.Context())
			req = req.WithContext(streamCtx)
			stopCancel := time.AfterFunc(500*time.Millisecond, cancel)
			defer func() { stopCancel.Stop(); cancel() }()
		}
		route, pathParams, err := router.FindRoute(req)
		if err != nil {
			t.Fatalf("route %s %s: %v", method, target, err)
		}
		request := req.Clone(context.Background())
		request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		request.ContentLength = int64(len(bodyBytes))
		requestOptions := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
		contentType := req.Header.Get("Content-Type")
		if contentType == "application/octet-stream" || contentType == "application/x-tar" {
			requestOptions.ExcludeRequestBody = true
		}
		requestInput := &openapi3filter.RequestValidationInput{Request: request, PathParams: pathParams, Route: route, Options: requestOptions}
		if err := openapi3filter.ValidateRequest(context.Background(), requestInput); err != nil {
			t.Errorf("request %s %s: %v", method, target, err)
		}
		rec := httptest.NewRecorder()
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		handler.ServeHTTP(rec, req)
		input := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: requestInput,
			Status:                 rec.Code, Header: rec.Header(), Body: io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		}
		if strings.HasSuffix(target, "/stream") && method == http.MethodGet {
			expectedEvent := "event: exit"
			if strings.HasSuffix(target, "/metrics/stream") {
				expectedEvent = "event: metrics"
			} else if target == "/v1/sessions/stream" {
				expectedEvent = "event: sessions"
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(rec.Body.String(), expectedEvent) {
				t.Errorf("invalid event stream response: content-type=%q body=%q", rec.Header().Get("Content-Type"), rec.Body.String())
			}
			input.Options = &openapi3filter.Options{ExcludeResponseBody: true}
		}
		if strings.HasPrefix(target, "/v1/sessions/") && strings.Contains(target, "/archive?") && method == http.MethodGet {
			if rec.Header().Get("Content-Type") != "application/x-tar" {
				t.Errorf("archive content type=%q", rec.Header().Get("Content-Type"))
			}
			tr := tar.NewReader(bytes.NewReader(rec.Body.Bytes()))
			for {
				if _, err := tr.Next(); err == io.EOF {
					break
				} else if err != nil {
					t.Errorf("invalid tar response: %v", err)
					break
				}
			}
			input.Options = &openapi3filter.Options{ExcludeResponseBody: true}
		}
		if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
			t.Errorf("response %s %s (%d): %v; body=%s", method, target, rec.Code, err, rec.Body.String())
		}
		return rec
	}

	check(http.MethodGet, "/healthz", "", false)
	check(http.MethodGet, "/metrics", "", false)
	check(http.MethodGet, "/v1/system", "", false)
	createdKey := check(http.MethodPost, "/v1/keys", `{"name":"contract key","scope":"session"}`, true)
	var apiKey struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(createdKey.Body.Bytes(), &apiKey); err != nil || apiKey.ID == "" || apiKey.Secret == "" {
		t.Fatalf("invalid create key response: key=%+v err=%v", apiKey, err)
	}
	keyList := check(http.MethodGet, "/v1/keys", "", true)
	if strings.Contains(keyList.Body.String(), apiKey.Secret) || strings.Contains(keyList.Body.String(), `"hash"`) {
		t.Fatal("key list leaked secret material")
	}
	scopedKeyReq := httptest.NewRequest(http.MethodGet, "/v1/system", nil)
	scopedKeyReq.Header.Set("Authorization", "Bearer "+apiKey.Secret)
	scopedKeyResponse := httptest.NewRecorder()
	handler.ServeHTTP(scopedKeyResponse, scopedKeyReq)
	if scopedKeyResponse.Code != http.StatusForbidden {
		t.Fatalf("session key system access status=%d body=%s", scopedKeyResponse.Code, scopedKeyResponse.Body.String())
	}
	check(http.MethodDelete, "/v1/keys/"+apiKey.ID, "", true)
	check(http.MethodDelete, "/v1/keys/key_missing", "", true)
	revokedKeyReq := httptest.NewRequest(http.MethodGet, "/v1/system", nil)
	revokedKeyReq.Header.Set("Authorization", "Bearer "+apiKey.Secret)
	revokedKeyResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokedKeyResponse, revokedKeyReq)
	if revokedKeyResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key access status=%d body=%s", revokedKeyResponse.Code, revokedKeyResponse.Body.String())
	}
	invalidKeyReq := httptest.NewRequest(http.MethodPost, "/v1/keys", strings.NewReader(`{"name":"bad scope","scope":"root"}`))
	invalidKeyReq.Header.Set("Authorization", "Bearer "+key.Secret)
	invalidKeyReq.Header.Set("Content-Type", "application/json")
	invalidKeyResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidKeyResponse, invalidKeyReq)
	if invalidKeyResponse.Code != http.StatusUnprocessableEntity || !strings.Contains(invalidKeyResponse.Body.String(), "key_scope_invalid") {
		t.Fatalf("invalid key scope status=%d body=%s", invalidKeyResponse.Code, invalidKeyResponse.Body.String())
	}
	created := check(http.MethodPost, "/v1/sessions", `{"image":"python:3.12","network_policy":{"mode":"none"}}`, true)
	var session SessionResponse
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.ID == "" {
		t.Fatal("create returned empty session id")
	}
	firstAuditPage := check(http.MethodGet, "/v1/sessions/"+session.ID+"/events?limit=1", "", true)
	var auditPage struct {
		Items      []auditEventResponse `json:"items"`
		NextCursor string               `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstAuditPage.Body.Bytes(), &auditPage); err != nil || len(auditPage.Items) != 1 || auditPage.NextCursor == "" {
		t.Fatalf("invalid audit event page: page=%+v err=%v", auditPage, err)
	}
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/events?limit=1&cursor="+auditPage.NextCursor, "", true)
	for i := 0; i < 2; i++ {
		check(http.MethodPost, "/v1/sessions", `{"image":"python:3.12","network_policy":{"mode":"none"}}`, true)
	}
	storedSessions, err := db.ListSessions(context.Background(), "", true)
	if err != nil || len(storedSessions) != 3 {
		t.Fatalf("test setup stored %d sessions: err=%v", len(storedSessions), err)
	}
	for _, target := range []string{"/v1/sessions?limit=0", "/v1/sessions?cursor=not-a-cursor"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+key.Secret)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid list query %s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	firstSessionPage := check(http.MethodGet, "/v1/sessions?limit=1", "", true)
	var sessionPage struct {
		Items      []SessionResponse `json:"items"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstSessionPage.Body.Bytes(), &sessionPage); err != nil || len(sessionPage.Items) != 1 || sessionPage.NextCursor == "" {
		t.Fatalf("invalid first session page: page=%+v err=%v", sessionPage, err)
	}
	seenSessions := map[string]bool{sessionPage.Items[0].ID: true}
	for page := 0; page < 2; page++ {
		response := check(http.MethodGet, "/v1/sessions?limit=1&cursor="+sessionPage.NextCursor, "", true)
		sessionPage = struct {
			Items      []SessionResponse `json:"items"`
			NextCursor string            `json:"next_cursor"`
		}{}
		if err := json.Unmarshal(response.Body.Bytes(), &sessionPage); err != nil || len(sessionPage.Items) != 1 {
			t.Fatalf("invalid session page %d: page=%+v err=%v", page+2, sessionPage, err)
		}
		if seenSessions[sessionPage.Items[0].ID] {
			t.Fatalf("session pagination repeated %s", sessionPage.Items[0].ID)
		}
		seenSessions[sessionPage.Items[0].ID] = true
		if page == 0 && sessionPage.NextCursor == "" {
			t.Fatal("middle session page omitted next_cursor")
		}
		if page == 1 && sessionPage.NextCursor != "" {
			t.Fatalf("final session page unexpectedly included next_cursor: seen=%v page=%+v", seenSessions, sessionPage)
		}
	}
	check(http.MethodGet, "/v1/sessions", "", true)
	check(http.MethodGet, "/v1/sessions/stream", "", true)
	check(http.MethodGet, "/v1/sessions/"+session.ID, "", true)
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/metrics", "", true)
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/metrics/stream", "", true)
	execPath := "/v1/sessions/" + session.ID + "/execs"
	started := check(http.MethodPost, execPath, `{"argv":["echo","hi"]}`, true)
	var execution struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &execution); err != nil {
		t.Fatal(err)
	}
	check(http.MethodGet, execPath, "", true)
	check(http.MethodGet, execPath+"/"+execution.ID, "", true)
	check(http.MethodPost, execPath+"/"+execution.ID+"/signal", `{"signal":15}`, true)
	waited := check(http.MethodPost, execPath+"?wait=true", `{"argv":["echo","wait"]}`, true)
	var waitedExec waitExecResponse
	if err := json.Unmarshal(waited.Body.Bytes(), &waitedExec); err != nil || waitedExec.Exec.ID == "" {
		t.Fatalf("invalid waited exec response: %+v err=%v", waitedExec, err)
	}
	check(http.MethodGet, execPath+"/"+waitedExec.Exec.ID+"/stream", "", true)
	firstExecPage := check(http.MethodGet, execPath+"?limit=1", "", true)
	var execPage struct {
		Items      []execResponse `json:"items"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstExecPage.Body.Bytes(), &execPage); err != nil || len(execPage.Items) != 1 || execPage.NextCursor == "" {
		t.Fatalf("invalid first exec page: page=%+v err=%v", execPage, err)
	}
	firstExecID := execPage.Items[0].ID
	secondExecPage := check(http.MethodGet, execPath+"?limit=1&cursor="+execPage.NextCursor, "", true)
	execPage = struct {
		Items      []execResponse `json:"items"`
		NextCursor string         `json:"next_cursor"`
	}{}
	if err := json.Unmarshal(secondExecPage.Body.Bytes(), &execPage); err != nil || len(execPage.Items) != 1 || execPage.Items[0].ID == firstExecID || execPage.NextCursor != "" {
		t.Fatalf("invalid second exec page: page=%+v err=%v", execPage, err)
	}
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/files/list", "", true)
	filePath := "/v1/sessions/" + session.ID + "/files?path=%2Ftmp%2Fopenapi.txt"
	check(http.MethodPut, filePath, "contract file", true)
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/files/list?path=%2Ftmp&limit=1", "", true)
	check(http.MethodGet, filePath, "", true)
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/files/list?path=%2Ftmp", "", true)
	check(http.MethodDelete, filePath, "", true)
	archivePath := "/v1/sessions/" + session.ID + "/archive?path=%2Ftmp"
	check(http.MethodGet, archivePath, "", true)
	check(http.MethodPut, archivePath, contractArchive(t, "archived.txt", "archive data"), true)
	check(http.MethodDelete, "/v1/sessions/"+session.ID, "", true)
}

func contractArchive(t *testing.T, name, contents string) string {
	t.Helper()
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return data.String()
}
