package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const tinyPGN = `[Event "Example chapter"]
[Result "*"]

1. e4 {Main idea} e5 2. Nf3 Nc6 (2... d6 {Alternative}) 3. Bb5 a6 *
`

func TestPGNRoundtripAndSelection(t *testing.T) {
	r, e := ParsePGN(tinyPGN)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Nodes) != 8 {
		t.Fatalf("nodes %d", len(r.Nodes))
	}
	cfg := DefaultConfig()
	cfg.Side = "white"
	cfg.TargetLines = 1
	s := Select(r, cfg)
	if len(s.Leaves) != 1 {
		t.Fatalf("target lines %d", len(s.Leaves))
	}
	out := filepath.Join(t.TempDir(), "chosen.pgn")
	if e = r.ExportFiltered(out); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(raw), "Main idea") {
		t.Fatal("lost comments")
	}
	if _, e = ParsePGN(string(raw)); e != nil {
		t.Fatalf("exported PGN invalid: %v", e)
	}
}
func TestServerOfflineAnalysis(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	srv, e := newServer()
	if e != nil {
		t.Fatal(e)
	}
	reqPayload := map[string]any{"side": "white", "mode": "full", "ratings": []string{"1800"}, "speeds": []string{"rapid"}, "eval_source": "none", "offline": true, "target_lines": 1, "min_chapter_lines": 1, "max_evals": 0, "pgn": map[string]string{"source": "upload", "name": "example.pgn", "text": tinyPGN}}
	raw, _ := json.Marshal(reqPayload)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/jobs", bytes.NewReader(raw))
	request.Host = "127.0.0.1:8765"
	request.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, request)
	if rr.Code != 200 {
		t.Fatalf("submit %d: %s", rr.Code, rr.Body.String())
	}
	var id struct {
		ID string `json:"id"`
	}
	if e = json.Unmarshal(rr.Body.Bytes(), &id); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 300; i++ {
		j := srv.job(id.ID)
		srv.mu.Lock()
		status := j.Status
		srv.mu.Unlock()
		if status == "completed" {
			break
		}
		if status == "error" {
			t.Fatal(srv.snapshot(j))
		}
		time.Sleep(10 * time.Millisecond)
	}
	j := srv.job(id.ID)
	srv.mu.Lock()
	status := j.Status
	srv.mu.Unlock()
	if status != "completed" {
		t.Fatalf("not completed: %s", status)
	}
	html, e := os.ReadFile(filepath.Join(j.Dir, "results", "dashboard.html"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(html, []byte("data:image/png;base64,")) {
		t.Fatal("missing embedded icon")
	}
	if !bytes.Contains(html, []byte("const DATA =")) {
		t.Fatal("missing report")
	}
}
