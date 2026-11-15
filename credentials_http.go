package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// The caller already checked the Host and Origin. Fetch metadata is a second
// protection against drive-by cross-site requests to the localhost service.
func (s *Server) credentialHandler(w http.ResponseWriter, r *http.Request) {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeErr(w, 403, fmt.Errorf("cross-site credential access denied"))
		return
	}
	if s.tokens == nil {
		writeErr(w, 500, fmt.Errorf("credential store unavailable"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		token, source, err := activeToken(s.tokens)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{
			"saved": token != "", "source": source,
			"storage":              s.tokens.Description(),
			"environment_override": strings.TrimSpace(os.Getenv("LICHESS_TOKEN")) != "",
		})
	case http.MethodPost:
		var request struct {
			Token string `json:"token"`
		}
		if err := readJSON(r, &request); err != nil {
			writeErr(w, 400, err)
			return
		}
		if err := s.tokens.Save(strings.TrimSpace(request.Token)); err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"saved": true, "storage": s.tokens.Description()})
	case http.MethodDelete:
		if err := s.tokens.Delete(); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"saved": false, "environment_override": strings.TrimSpace(os.Getenv("LICHESS_TOKEN")) != ""})
	default:
		writeErr(w, 405, fmt.Errorf("method not allowed"))
	}
}
