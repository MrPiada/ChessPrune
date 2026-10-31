package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html dashboard_template.html assets/ChessPrune_Icon.png
var embedded embed.FS
var version = "dev"
var idRE = regexp.MustCompile(`^[A-Za-z0-9_-]{4,72}$`)

type Progress struct {
	Phase   string  `json:"phase"`
	Percent float64 `json:"percent"`
	Done    int     `json:"done"`
	Total   int     `json:"total"`
	Message string  `json:"message"`
}
type Job struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Status          string             `json:"status"`
	Created         string             `json:"created"`
	Ended           string             `json:"ended,omitempty"`
	Config          Config             `json:"config"`
	Progress        Progress           `json:"progress"`
	Files           map[string]string  `json:"files,omitempty"`
	Log             string             `json:"log"`
	Error           string             `json:"error,omitempty"`
	CancelRequested bool               `json:"cancel_requested,omitempty"`
	Cancel          context.CancelFunc `json:"-"`
	Dir             string             `json:"-"`
}
type Server struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	active string
	root   string
}

func newServer() (*Server, error) {
	root := ""
	if p, e := os.UserConfigDir(); e == nil {
		root = filepath.Join(p, "ChessPrune", "analyses")
	} else {
		root = filepath.Join(os.TempDir(), "ChessPrune", "analyses")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Server{jobs: map[string]*Job{}, root: root}, nil
}
func (s *Server) snapshot(j *Job) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{"id": j.ID, "name": j.Name, "status": j.Status, "created": j.Created, "config": j.Config, "progress": j.Progress, "log": j.Log, "files": j.Files}
	if j.Error != "" {
		out["error"] = j.Error
	}
	if j.Ended != "" {
		out["ended"] = j.Ended
	}
	return out
}
func (s *Server) job(id string) *Job { s.mu.Lock(); defer s.mu.Unlock(); return s.jobs[id] }
func (s *Server) postJob(w http.ResponseWriter, req *http.Request) {
	var payload struct {
		Config
		Token string `json:"token"`
		PGN   struct {
			Source string `json:"source"`
			Name   string `json:"name"`
			Text   string `json:"text"`
		} `json:"pgn"`
	}
	payload.Config = DefaultConfig()
	if err := readJSON(req, &payload); err != nil {
		writeErr(w, 400, err)
		return
	}
	cfg := payload.Config
	if e := cfg.Validate(); e != nil {
		writeErr(w, 400, e)
		return
	}
	if payload.PGN.Source != "upload" || !strings.HasSuffix(strings.ToLower(payload.PGN.Name), ".pgn") || strings.TrimSpace(payload.PGN.Text) == "" || len(payload.PGN.Text) > 7*1024*1024 {
		writeErr(w, 400, fmt.Errorf("choose a valid PGN file (maximum 7 MB)"))
		return
	}
	if cfg.EvalSource == "local" || cfg.EvalSource == "hybrid" {
		if findEngine(cfg.Engine) == "" {
			writeErr(w, 400, fmt.Errorf("Stockfish not found; select Auto, Cloud or None, or configure the engine path"))
			return
		}
	}
	s.mu.Lock()
	if s.active != "" {
		s.mu.Unlock()
		writeErr(w, 400, fmt.Errorf("another analysis is already running"))
		return
	}
	random := make([]byte, 3)
	_, _ = rand.Read(random)
	id := time.Now().Format("20060102_150405") + "_" + hex.EncodeToString(random)
	dir := filepath.Join(s.root, id)
	if err := os.MkdirAll(filepath.Join(dir, "results"), 0700); err != nil {
		s.mu.Unlock()
		writeErr(w, 500, err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "input_repertoire.pgn"), []byte(payload.PGN.Text), 0600); err != nil {
		s.mu.Unlock()
		writeErr(w, 500, err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{ID: id, Name: filepath.Base(payload.PGN.Name), Status: "queued", Created: time.Now().Format(time.RFC3339), Config: cfg, Progress: Progress{Phase: "queued", Message: "Waiting to start"}, Files: map[string]string{}, Dir: dir, Cancel: cancel}
	s.active = id
	s.jobs[id] = j
	s.mu.Unlock()
	go s.run(ctx, j, payload.PGN.Text, payload.Token)
	writeJSON(w, 200, map[string]any{"id": id, "job": s.snapshot(j)})
}
func (s *Server) run(ctx context.Context, j *Job, content, token string) {
	logFile, err := os.Create(filepath.Join(j.Dir, "console.log"))
	if err != nil {
		log.Printf("Cannot create job log: %v", err)
		return
	}
	defer logFile.Close()
	writer := io.MultiWriter(logFile, log.Writer())
	logger := log.New(writer, "", log.LstdFlags)
	s.mu.Lock()
	j.Status = "running"
	j.Progress = Progress{Phase: "explorer", Percent: 1, Message: "Parsing repertoire"}
	s.mu.Unlock()
	onProgress := func(phase string, done, total int, percent float64, message string) {
		s.mu.Lock()
		j.Progress = Progress{Phase: phase, Done: done, Total: total, Percent: percent, Message: message}
		s.mu.Unlock()
	}
	result := ExecuteAnalysis(ctx, content, j.Config, token, filepath.Join(j.Dir, "results"), logger, onProgress)
	s.mu.Lock()
	if result != nil {
		j.Error = result.Error()
		if errors.Is(result, context.Canceled) || j.CancelRequested {
			j.Status = "cancelled"
		} else {
			j.Status = "error"
		}
	} else {
		j.Status = "completed"
		j.Progress = Progress{"done", 100, 1, 1, "Analysis completed"}
		out := "/files/" + j.ID + "/"
		j.Files = map[string]string{"dashboard.html": out + "dashboard.html", "data.json": out + "data.json", "filtered_repertoire.pgn": out + "filtered_repertoire.pgn"}
		matches, _ := filepath.Glob(filepath.Join(j.Dir, "results", "target_repertoire_*_lines.pgn"))
		if len(matches) > 0 {
			j.Files["target_pgn"] = out + filepath.Base(matches[0])
		}
	}
	j.Ended = time.Now().Format(time.RFC3339)
	s.active = ""
	j.Cancel = nil
	s.mu.Unlock()
	if result != nil {
		logger.Printf("ANALYSIS ERROR: %v", result)
	}
}
func (s *Server) getJob(w http.ResponseWriter, id string) {
	j := s.job(id)
	if j == nil {
		writeErr(w, 404, fmt.Errorf("analysis not found"))
		return
	}
	view := s.snapshot(j)
	raw, _ := os.ReadFile(filepath.Join(j.Dir, "console.log"))
	if len(raw) > 48000 {
		raw = raw[len(raw)-48000:]
	}
	view["log"] = string(raw)
	writeJSON(w, 200, view)
}
func (s *Server) cancelJob(w http.ResponseWriter, id string) {
	s.mu.Lock()
	j := s.jobs[id]
	if j == nil || s.active != id {
		s.mu.Unlock()
		writeErr(w, 400, fmt.Errorf("analysis not running"))
		return
	}
	j.CancelRequested = true
	cancel := j.Cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (s *Server) serveFile(w http.ResponseWriter, req *http.Request) {
	bits := strings.Split(strings.TrimPrefix(req.URL.Path, "/files/"), "/")
	if len(bits) != 2 || !idRE.MatchString(bits[0]) {
		http.NotFound(w, req)
		return
	}
	j := s.job(bits[0])
	if j == nil {
		http.NotFound(w, req)
		return
	}
	name := bits[1]
	allowed := name == "dashboard.html" || name == "data.json" || name == "filtered_repertoire.pgn" || (strings.HasPrefix(name, "target_repertoire_") && strings.HasSuffix(name, "_lines.pgn") && len(name) < 80)
	if !allowed {
		http.NotFound(w, req)
		return
	}
	data, err := os.ReadFile(filepath.Join(j.Dir, "results", name))
	if err != nil {
		http.NotFound(w, req)
		return
	}
	if strings.HasSuffix(name, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else if strings.HasSuffix(name, ".json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/x-chess-pgn; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	}
	_, _ = w.Write(data)
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:") || host == "localhost" || host == "127.0.0.1" {
	} else {
		writeErr(w, 403, fmt.Errorf("unauthorized host"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	if r.Method == "POST" {
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			writeErr(w, 403, fmt.Errorf("unauthorized origin"))
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeErr(w, 415, fmt.Errorf("JSON request required"))
			return
		}
	}
	path := r.URL.Path
	switch {
	case r.Method == "GET" && (path == "/" || path == "/index.html"):
		b, _ := embedded.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	case r.Method == "GET" && path == "/assets/ChessPrune_Icon.png":
		b, _ := embedded.ReadFile("assets/ChessPrune_Icon.png")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b)
	case r.Method == "GET" && path == "/api/health":
		engine := findEngine("")
		writeJSON(w, 200, map[string]any{"engine": engine, "runtime": "Go " + runtime.Version(), "chess_installed": true, "requests_installed": true, "go_backend": true})
	case r.Method == "GET" && path == "/api/jobs":
		writeJSON(w, 200, map[string]any{"jobs": []any{}})
	case r.Method == "GET" && strings.HasPrefix(path, "/api/jobs/"):
		s.getJob(w, strings.TrimPrefix(path, "/api/jobs/"))
	case r.Method == "GET" && strings.HasPrefix(path, "/files/"):
		s.serveFile(w, r)
	case r.Method == "POST" && path == "/api/jobs":
		s.postJob(w, r)
	case r.Method == "POST" && strings.HasPrefix(path, "/api/cancel/"):
		s.cancelJob(w, strings.TrimPrefix(path, "/api/cancel/"))
	default:
		http.NotFound(w, r)
	}
}
func readJSON(r *http.Request, obj any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 15*1024*1024)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if e := decoder.Decode(obj); e != nil {
		return fmt.Errorf("invalid JSON: %w", e)
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
func main() {
	port := flag.Int("port", 8765, "local HTTP port")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser automatically")
	showVersion := flag.Bool("version", false, "print release version")
	analyze := flag.String("analyze", "", "analyze a PGN from the command line")
	side := flag.String("side", "black", "repertoire side: white or black (CLI)")
	evalSource := flag.String("eval-source", "auto", "local, cloud, hybrid, auto or none (CLI)")
	offline := flag.Bool("offline", false, "only use cached Lichess data (CLI)")
	target := flag.Int("target-lines", 50, "target study lines (CLI)")
	out := flag.String("out", "results", "output directory (CLI)")
	flag.Parse()
	if *showVersion {
		fmt.Println("ChessPrune", version)
		return
	}
	if *analyze != "" {
		data, e := os.ReadFile(*analyze)
		if e != nil {
			log.Fatal(e)
		}
		cfg := DefaultConfig()
		cfg.Mode = "full"
		cfg.MaxEvals = 0
		cfg.Side = *side
		cfg.EvalSource = *evalSource
		cfg.Offline = *offline
		cfg.TargetLines = *target
		logger := log.New(os.Stdout, "", log.LstdFlags)
		progress := func(phase string, done, total int, p float64, msg string) {
			logger.Printf("%s: %.1f%% %d/%d %s", phase, p, done, total, msg)
		}
		if e = ExecuteAnalysis(context.Background(), string(data), cfg, os.Getenv("LICHESS_TOKEN"), *out, logger, progress); e != nil {
			log.Fatal(e)
		}
		return
	}
	srv, e := newServer()
	if e != nil {
		log.Fatal(e)
	}
	var ln net.Listener
	for p := *port; p < *port+30; p++ {
		ln, e = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if e == nil {
			break
		}
	}
	if e != nil {
		log.Fatal(e)
	}
	url := "http://" + ln.Addr().String() + "/"
	log.Printf("ChessPrune %s | %s | Go backend | local access only", version, url)
	if !*noBrowser {
		go func() { time.Sleep(600 * time.Millisecond); openBrowser(url) }()
	}
	if e = http.Serve(ln, srv); e != nil {
		log.Fatal(e)
	}
}
