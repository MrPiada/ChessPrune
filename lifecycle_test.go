package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserLifecycleLastTabAndReloadGrace(t *testing.T) {
	var exits atomic.Int32
	lc := newBrowserLifecycle(true, 5*time.Second, func() { exits.Add(1) })
	first, _, err := lc.register()
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := lc.register()
	if err != nil {
		t.Fatal(err)
	}
	lc.drop(first)
	if lc.checkExit(time.Now().Add(40 * time.Second)) {
		t.Fatal("exited with a tab open")
	}
	lc.drop(second)
	if lc.checkExit(time.Now().Add(time.Second)) {
		t.Fatal("exited before grace")
	}
	third, _, err := lc.register()
	if err != nil {
		t.Fatal(err)
	}
	if lc.checkExit(time.Now().Add(40 * time.Second)) {
		t.Fatal("exited after reload")
	}
	lc.drop(third)
	if !lc.checkExit(time.Now().Add(6 * time.Second)) {
		t.Fatal("did not exit after last tab")
	}
	deadline := time.Now().Add(time.Second)
	for exits.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if exits.Load() != 1 {
		t.Fatalf("exit count = %d", exits.Load())
	}
}

func TestBrowserLifecycleHeadlessDoesNotAutoExit(t *testing.T) {
	lc := newBrowserLifecycle(false, time.Millisecond, func() { t.Error("headless should stay running") })
	id, _, err := lc.register()
	if err != nil {
		t.Fatal(err)
	}
	lc.drop(id)
	if lc.checkExit(time.Now().Add(time.Hour)) {
		t.Fatal("headless exited")
	}
	idle := newBrowserLifecycle(true, time.Millisecond, nil)
	if idle.checkExit(time.Now().Add(time.Hour)) {
		t.Fatal("exited without any UI opening")
	}
}

func TestBrowserLifecycleSSECloseAndQuit(t *testing.T) {
	var exits atomic.Int32
	lc := newBrowserLifecycle(true, 100*time.Millisecond, func() { exits.Add(1) })
	ts := httptest.NewServer(lc)
	defer ts.Close()
	response, err := ts.Client().Get(ts.URL + "/api/lifecycle/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buf := make([]byte, 100)
	n, err := response.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	data := string(buf[:n])
	if !strings.Contains(data, "event: session") {
		t.Fatalf("SSE: %q", data)
	}
	start := strings.Index(data, "data: ")
	if start < 0 {
		t.Fatal(data)
	}
	id := strings.TrimSpace(strings.SplitN(data[start+6:], "\n", 2)[0])
	request, err := http.NewRequestWithContext(context.Background(), "POST", ts.URL+"/api/lifecycle/quit", strings.NewReader(`{"session":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	r, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("quit status %d", r.StatusCode)
	}
	deadline := time.Now().Add(time.Second)
	for exits.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if exits.Load() != 1 {
		t.Fatalf("exit count %d", exits.Load())
	}
	// A cross-site request must not be allowed to quit the app.
	cross, _ := http.NewRequest("POST", ts.URL+"/api/lifecycle/quit", strings.NewReader(`{"session":"`+id+`"}`))
	cross.Header.Set("Content-Type", "application/json")
	cross.Header.Set("Origin", "https://malicious.example")
	resp, err := ts.Client().Do(cross)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross site status: %d", resp.StatusCode)
	}
}
