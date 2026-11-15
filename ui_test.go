package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnifiedWorkspaceIsEmbedded(t *testing.T) {
	b, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, value := range []string{"id=\"analysisTab\"", "id=\"graphTab\"", "id=\"graphFrame\"", "id=\"engineInstall\"", "id=\"checkEngine\"", "embedded=1", "data-local-engine"} {
		if !strings.Contains(s, value) {
			t.Errorf("Missing UI feature %s", value)
		}
	}
}

func TestEngineEndpointAndHealth(t *testing.T) {
	s := &Server{jobs: map[string]*Job{}}
	for _, path := range []string{"/api/health", "/api/engine", "/api/engine?path=%2Fnonexistent%2Fstockfish"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8765"+path, nil)
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, req)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "\"platform\"") {
			t.Errorf("unexpected response to %s: %d %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestDashboardCanEmbedAsSecondTab(t *testing.T) {
	b, err := embedded.ReadFile("dashboard_template.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"id=\"embeddedNav\"", "classList.add('embedded','view-tree')", "view-lines", "view-traps", "view-data"} {
		if !strings.Contains(string(b), value) {
			t.Errorf("missing dashboard view: %s", value)
		}
	}
}
