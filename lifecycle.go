package main

// The browser is the application's window. A single persistent SSE connection
// per top-level UI tab lets the Go process know when the last window closes,
// including on macOS where "open URL" launches an external browser.
// Refreshes and temporary network interruptions are tolerated by a grace period.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type browserLifecycle struct {
	mu         sync.Mutex
	clients    map[string]chan struct{}
	seenWindow bool
	emptySince time.Time
	exitOnce   sync.Once
	autoExit   bool
	grace      time.Duration
	exit       func()
}

func newBrowserLifecycle(autoExit bool, grace time.Duration, exit func()) *browserLifecycle {
	return &browserLifecycle{
		clients:  make(map[string]chan struct{}),
		autoExit: autoExit,
		grace:    grace,
		exit:     exit,
	}
}

func (l *browserLifecycle) triggerExit() {
	l.exitOnce.Do(func() {
		if l.exit != nil {
			go l.exit()
		}
	})
}

// checkExit is deliberately separate from the ticker, so it can be tested
// deterministically without browser timing or network sleeps.
func (l *browserLifecycle) checkExit(now time.Time) bool {
	l.mu.Lock()
	ready := l.autoExit && l.seenWindow && len(l.clients) == 0 && !l.emptySince.IsZero() && now.Sub(l.emptySince) >= l.grace
	l.mu.Unlock()
	if ready {
		l.triggerExit()
	}
	return ready
}

func (l *browserLifecycle) watch(ctx context.Context) {
	timer := time.NewTicker(500 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-timer.C:
			l.checkExit(t)
		}
	}
}

func (l *browserLifecycle) drop(id string) {
	l.mu.Lock()
	if ch, ok := l.clients[id]; ok {
		delete(l.clients, id)
		close(ch)
		if len(l.clients) == 0 {
			l.emptySince = time.Now()
		}
	}
	l.mu.Unlock()
}

func (l *browserLifecycle) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, ch := range l.clients {
		delete(l.clients, id)
		close(ch)
	}
	l.emptySince = time.Now()
}

func (l *browserLifecycle) register() (string, <-chan struct{}, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	id := hex.EncodeToString(b)
	l.mu.Lock()
	ch := make(chan struct{})
	l.clients[id] = ch
	l.seenWindow = true
	l.emptySince = time.Time{}
	l.mu.Unlock()
	return id, ch, nil
}

func localLifecycleRequest(r *http.Request) bool {
	host := strings.ToLower(r.Host)
	if !(host == "localhost" || strings.HasPrefix(host, "localhost:") || host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.1:")) {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	return true
}

func (l *browserLifecycle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !localLifecycleRequest(r) {
		http.Error(w, "local same-origin access only", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	switch r.URL.Path {
	case "/api/lifecycle/events":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unavailable", 500)
			return
		}
		id, closed, err := l.register()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer l.drop(id)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("X-Accel-Buffering", "no")
		_, _ = fmt.Fprintf(w, "event: session\ndata: %s\n\n", id)
		flusher.Flush()
		heartbeat := time.NewTicker(10 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-closed:
				return
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	case "/api/lifecycle/close", "/api/lifecycle/quit":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON required", 415)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		var req struct {
			Session string `json:"session"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		l.mu.Lock()
		_, known := l.clients[req.Session]
		l.mu.Unlock()
		if !known {
			http.Error(w, errors.New("unknown browser session").Error(), 403)
			return
		}
		l.drop(req.Session)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		if r.URL.Path == "/api/lifecycle/quit" {
			l.triggerExit()
		}
	default:
		http.NotFound(w, r)
	}
}
