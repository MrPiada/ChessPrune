package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialsPersistenceAndAPI(t *testing.T) {
	t.Setenv("LICHESS_TOKEN", "")
	path := filepath.Join(t.TempDir(), "settings", "lichess-token")
	s := &Server{jobs: map[string]*Job{}, tokens: fileTokenStore{path: path}}
	call := func(method, body string, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8765/api/credentials", strings.NewReader(body))
		if method == http.MethodPost || method == http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, req)
		return rr
	}
	if r := call("POST", `{"token":"my_token_123"}`, "http://127.0.0.1:8765"); r.Code != 200 {
		t.Fatalf("save %d %s", r.Code, r.Body.String())
	}
	if b, err := os.ReadFile(path); err != nil || strings.TrimSpace(string(b)) != "my_token_123" {
		t.Fatalf("saved credential mismatch (error: %v)", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm()&0077 != 0 {
		t.Fatalf("credential file has permissive permissions: %o", info.Mode().Perm())
	}
	// This simulates a new process / new token store instance.
	loaded, err := (fileTokenStore{path: path}).Load()
	if err != nil || loaded != "my_token_123" {
		t.Fatalf("token did not survive store recreation: %q, %v", loaded, err)
	}
	r := call("GET", "", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"saved":true`) || strings.Contains(r.Body.String(), "my_token_123") {
		t.Fatalf("credential status disclosed a secret or status wrong: %d %s", r.Code, r.Body.String())
	}
	if r := call("POST", `{"token":"another_token"}`, "https://other.example"); r.Code != 403 {
		t.Fatalf("cross-origin save accepted: %d", r.Code)
	}
	if r := call("POST", `{"token":"not valid !"}`, ""); r.Code != 400 {
		t.Fatalf("invalid credential accepted: %d", r.Code)
	}
	if r := call("DELETE", `{}`, "http://127.0.0.1:8765"); r.Code != 200 {
		t.Fatalf("forget %d %s", r.Code, r.Body.String())
	}
	if r := call("GET", "", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"saved":false`) {
		t.Fatalf("credential still saved: %s", r.Body.String())
	}
}

func TestCredentialsEnvironmentOverride(t *testing.T) {
	t.Setenv("LICHESS_TOKEN", "override_token")
	store := fileTokenStore{path: filepath.Join(t.TempDir(), "token")}
	if err := store.Save("stored_token"); err != nil {
		t.Fatal(err)
	}
	got, source, err := activeToken(store)
	if err != nil || got != "override_token" || source != "environment" {
		t.Fatalf("unexpected credential selection %q %q %v", got, source, err)
	}
}

type captureRoundTripper struct{ seen map[string]string }

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.seen[req.URL.Hostname()] = req.Header.Get("Authorization")
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`{"white":10,"draws":3,"black":5,"moves":[]}`)), Header: http.Header{}, Request: req}, nil
}
func TestLichessTokenOnlyForLichessHost(t *testing.T) {
	rt := &captureRoundTripper{seen: map[string]string{}}
	c := &Client{Cache: t.TempDir(), Token: "test_secret", HTTP: &http.Client{Transport: rt}, Logger: log.New(io.Discard, "", 0)}
	params := map[string]string{"fen": "initial", "variant": "standard"}
	for _, check := range []struct{ service, endpoint string }{
		{"explorer", "https://explorer.lichess.org/lichess"},
		{"cloud", "https://lichess.org/api/cloud-eval"},
		{"cloud_other_host", "https://example.net/fake-cloud"},
	} {
		if _, err := c.Fetch(context.Background(), check.service, check.endpoint, params); err != nil {
			t.Fatal(err)
		}
	}
	if rt.seen["explorer.lichess.org"] != "" || rt.seen["example.net"] != "" || rt.seen["lichess.org"] != "Bearer test_secret" {
		t.Fatalf("credential routing wrong: %+v", rt.seen)
	}
}

func TestBoardUsesFixedSVGAndEmbeddedPieces(t *testing.T) {
	b, err := embedded.ReadFile("dashboard_template.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, required := range []string{`viewBox="0 0 800 800"`, `width="100" height="100"`, `const pieceImages=`, `data:image/svg+xml;base64,`, `function showBoard(){`} {
		if !strings.Contains(s, required) {
			t.Errorf("missing fixed board component: %s", required)
		}
	}
	if strings.Contains(s, "sq+=`<div") {
		t.Error("legacy unstable square markup remains")
	}
}

// A JSON marshalled job must never expose an authentication token.
func TestJobViewDoesNotContainCredential(t *testing.T) {
	s := &Server{jobs: map[string]*Job{}}
	b, _ := json.Marshal(s.snapshot(&Job{ID: "job", Config: DefaultConfig()}))
	if bytes.Contains(bytes.ToLower(b), []byte(`"token"`)) {
		t.Fatal("job snapshot includes a token field")
	}
}
