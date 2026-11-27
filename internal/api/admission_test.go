package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

func TestAdmissionQueueFIFOOrderAndTimeout(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "admission.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := auth.Generate("key_admin", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{
		ID:        admin.ID,
		Name:      admin.Name,
		Prefix:    admin.Prefix,
		Hash:      admin.Hash,
		Scope:     admin.Scope,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Capacity.MemoryOvercommit = 1
	cfg.Capacity.CPUOvercommit = 1
	cfg.Capacity.QueueWaitS = 1

	backend := &fakeRuntime{boxes: map[string]*fakeSandbox{}}
	srv := &server{
		store:          db,
		backend:        backend,
		backendName:    "fake",
		mux:            http.NewServeMux(),
		cfg:            cfg,
		activeExecs:    map[string]runtime.ExecHandle{},
		terminals:      map[string]*terminalHub{},
		idemLocks:      map[string]*idemLock{},
		rateCounts:     map[string]rateWindow{},
		httpRequests:   map[httpMetricKey]uint64{},
		httpDurations:  map[httpRouteKey]*httpDurationHist{},
		hostCapacityFn: func() (hostCapacity, error) { return hostCapacity{memoryMB: 512, cpuMilli: 1000, pids: 256}, nil },
	}
	srv.mux.HandleFunc("POST /v1/sessions", srv.createSession)
	srv.mux.HandleFunc("DELETE /v1/sessions/{id}", srv.deleteSession)

	doCreate := func(ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{"image":"alpine:latest","limits":{"cpu_millicores":1000,"memory_mb":512,"pids":256}}`))
		r = r.WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+admin.Secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, r)
		return w
	}

	// First session consumes all 512 MB / 1000 mCPU capacity.
	first := doCreate(context.Background())
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status=%d body=%s", first.Code, first.Body.String())
	}
	var firstResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatal(err)
	}

	var orderMu sync.Mutex
	var order []string
	var wg sync.WaitGroup
	wg.Add(2)

	var secondCode, thirdCode int
	var thirdRetryAfter string

	// Enqueue second request first, then third request, and verify FIFO wake order.
	go func() {
		defer wg.Done()
		res := doCreate(context.Background())
		secondCode = res.Code
		orderMu.Lock()
		order = append(order, "second")
		orderMu.Unlock()
	}()
	time.Sleep(50 * time.Millisecond)

	go func() {
		defer wg.Done()
		res := doCreate(context.Background())
		thirdCode = res.Code
		thirdRetryAfter = res.Header().Get("Retry-After")
		orderMu.Lock()
		order = append(order, "third")
		orderMu.Unlock()
	}()
	time.Sleep(100 * time.Millisecond)

	// Destroy first session; second should wake and succeed, third should wait and time out with 503.
	delReq := httptest.NewRequest(http.MethodDelete, "/v1/sessions/"+firstResp.ID, nil)
	delReq.Header.Set("Authorization", "Bearer "+admin.Secret)
	delW := httptest.NewRecorder()
	srv.mux.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", delW.Code, delW.Body.String())
	}

	wg.Wait()

	if secondCode != http.StatusCreated {
		t.Fatalf("second queued request status=%d, want 201", secondCode)
	}
	if thirdCode != http.StatusServiceUnavailable || thirdRetryAfter != "1" {
		t.Fatalf("third queued request status=%d Retry-After=%q, want 503 / 1", thirdCode, thirdRetryAfter)
	}
	if len(order) != 2 || order[0] != "second" || order[1] != "third" {
		t.Fatalf("unexpected FIFO completion order: %v", order)
	}
}

func TestAdmissionQueueContextCancelUnblocksNext(t *testing.T) {
	var q admissionQueue
	first := q.enqueue()
	second := q.enqueue()
	third := q.enqueue()

	select {
	case <-first.turn:
	default:
		t.Fatal("first waiter should immediately hold turn")
	}
	select {
	case <-second.turn:
		t.Fatal("second waiter should not hold turn yet")
	default:
	}

	// Cancel/dequeue middle waiter before first finishes.
	q.dequeue(second)
	select {
	case <-third.turn:
		t.Fatal("third waiter should still wait for first")
	default:
	}

	// First finishes; third should immediately receive turn.
	q.dequeue(first)
	select {
	case <-third.turn:
	case <-time.After(time.Second):
		t.Fatal("third waiter did not receive turn after first dequeued")
	}
	q.dequeue(third)
}
