package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func findEngine(name string) string {
	candidates := []string{}
	if name != "" {
		candidates = append(candidates, name)
	} else {
		candidates = append(candidates, "stockfish")
		if p, e := os.Executable(); e == nil {
			dir := filepath.Dir(p)
			names := []string{"stockfish", "stockfish.exe"}
			for _, n := range names {
				candidates = append(candidates, filepath.Join(dir, "engines", n), filepath.Join(dir, n))
			}
		}
		candidates = append(candidates, "/opt/homebrew/bin/stockfish", "/usr/local/bin/stockfish", "/usr/bin/stockfish")
	}
	for _, candidate := range candidates {
		p, e := exec.LookPath(candidate)
		if e == nil {
			return p
		}
	}
	return ""
}

type uciEngine struct {
	cmd       *exec.Cmd
	input     io.WriteCloser
	out       chan string
	readerErr chan error
}

func launchEngine(ctx context.Context, path string, threads, hash int) (*uciEngine, error) {
	cmd := exec.CommandContext(ctx, path)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	e := &uciEngine{cmd, in, make(chan string, 512), make(chan error, 1)}
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			select {
			case e.out <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		e.readerErr <- scanner.Err()
		close(e.out)
	}()
	if _, err = e.input.Write([]byte("uci\n")); err != nil {
		return nil, err
	}
	if err = e.until(ctx, "uciok"); err != nil {
		return nil, err
	}
	fmt.Fprintf(e.input, "setoption name Threads value %d\nsetoption name Hash value %d\nisready\n", threads, hash)
	if err = e.until(ctx, "readyok"); err != nil {
		return nil, err
	}
	return e, nil
}
func (e *uciEngine) until(ctx context.Context, word string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-e.out:
			if !ok {
				return fmt.Errorf("Stockfish terminated before %s", word)
			}
			if line == word {
				return nil
			}
		}
	}
}
func (e *uciEngine) analyze(ctx context.Context, fen string, whiteTurn bool, depth int) (*Evaluation, error) {
	if _, err := fmt.Fprintf(e.input, "position fen %s\ngo depth %d\n", fen, depth); err != nil {
		return nil, err
	}
	var cp, mate *int
	gotDepth := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case line, ok := <-e.out:
			if !ok {
				return nil, fmt.Errorf("Stockfish terminated unexpectedly")
			}
			if strings.HasPrefix(line, "info ") {
				fields := strings.Fields(line)
				for i := 1; i+1 < len(fields); i++ {
					if fields[i] == "depth" {
						d, _ := strconv.Atoi(fields[i+1])
						gotDepth = d
					}
					if fields[i] == "score" && i+2 < len(fields) {
						v, _ := strconv.Atoi(fields[i+2])
						if !whiteTurn {
							v = -v
						}
						if fields[i+1] == "cp" {
							cp = &v
							mate = nil
						}
						if fields[i+1] == "mate" {
							mate = &v
							x := 100000
							if v < 0 {
								x = -100000
							}
							cp = &x
						}
					}
				}
			}
			if strings.HasPrefix(line, "bestmove ") {
				if cp == nil {
					return nil, fmt.Errorf("Stockfish returned no evaluation for %s", fen)
				}
				return &Evaluation{CP: cp, Mate: mate, Depth: gotDepth, Source: "local"}, nil
			}
		}
	}
}
func (e *uciEngine) close() {
	_, _ = io.WriteString(e.input, "quit\n")
	_ = e.input.Close()
	_ = e.cmd.Wait()
}
func (r *Repertoire) Evaluate(ctx context.Context, c *Client, cfg Config, progress func(string, int, int, float64, string), logger *log.Logger) (int, int, error) {
	source := cfg.EvalSource
	if source == "none" {
		progress("engine", 0, 0, 95, "Engine evaluations disabled")
		return 0, 0, nil
	}
	enginePath := findEngine(cfg.Engine)
	if source == "auto" {
		if enginePath != "" {
			source = "local"
		} else {
			source = "cloud"
		}
	}
	if (source == "local" || source == "hybrid") && enginePath == "" {
		return 0, 0, fmt.Errorf("Stockfish not installed; select Cloud or Auto, or provide a Stockfish executable")
	}
	// Prioritize opponent move errors, including the repertoire reply.
	type edge struct {
		n      int64
		pk, ck string
	}
	edges := []edge{}
	for _, child := range r.Nodes {
		if child.Parent == nil {
			continue
		}
		parent := r.Nodes[*child.Parent]
		if parent.Turn == cfg.Side {
			continue
		}
		edges = append(edges, edge{child.Stats.total(), parent.Key, child.Key})
	}
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].n > edges[j].n })
	selected := []string{}
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] {
			selected = append(selected, k)
			seen[k] = true
		}
	}
	for _, e := range edges {
		keys := []string{e.pk, e.ck}
		for _, idx := range r.ByKey[e.ck] {
			n := r.Nodes[idx]
			for _, id := range n.Children[:min(len(n.Children), cfg.RepertoireOptions)] {
				keys = append(keys, r.Nodes[id].Key)
			}
		}
		need := 0
		groupSeen := map[string]bool{}
		for _, k := range keys {
			if !seen[k] && !groupSeen[k] {
				need++
				groupSeen[k] = true
			}
		}
		if cfg.MaxEvals > 0 && len(selected)+need > cfg.MaxEvals {
			continue
		}
		for _, k := range keys {
			add(k)
		}
	}
	for _, e := range edges {
		if !seen[e.ck] {
			continue
		}
		for _, idx := range r.ByKey[e.ck] {
			for _, id := range r.Nodes[idx].Children {
				if cfg.MaxEvals > 0 && len(selected) >= cfg.MaxEvals {
					break
				}
				add(r.Nodes[id].Key)
			}
		}
	}
	ranked := make([]string, 0, len(r.ByKey))
	for k := range r.ByKey {
		ranked = append(ranked, k)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		freq := func(k string) int64 {
			n := int64(0)
			for _, idx := range r.ByKey[k] {
				n = max(n, r.Nodes[idx].Stats.total())
			}
			return n
		}
		a, b := freq(ranked[i]), freq(ranked[j])
		if a != b {
			return a > b
		}
		return ranked[i] < ranked[j]
	})
	for _, k := range ranked {
		if cfg.MaxEvals > 0 && len(selected) >= cfg.MaxEvals {
			break
		}
		add(k)
	}
	progress("engine", 0, len(selected), 55, "Evaluating chess positions")
	localCache := map[string]*Evaluation{}
	cacheFile := ""
	if source == "local" || source == "hybrid" {
		h := sha256.Sum256([]byte(enginePath))
		cacheFile = filepath.Join(c.Cache, fmt.Sprintf("go_stockfish_%s_depth%d.json", hex.EncodeToString(h[:6]), cfg.Depth))
		if raw, e := os.ReadFile(cacheFile); e == nil {
			_ = json.Unmarshal(raw, &localCache)
		}
	}
	saveCache := func() {
		if cacheFile != "" {
			raw, _ := json.Marshal(localCache)
			_ = os.WriteFile(cacheFile+".tmp", raw, 0600)
			_ = os.Rename(cacheFile+".tmp", cacheFile)
		}
	}
	var engine *uciEngine
	defer func() {
		if engine != nil {
			engine.close()
		}
		saveCache()
	}()
	if source == "local" || source == "hybrid" {
		missing := false
		for _, k := range selected {
			if _, ok := localCache[k]; !ok {
				missing = true
				break
			}
		}
		if missing {
			var err error
			engine, err = launchEngine(ctx, enginePath, cfg.Threads, cfg.HashMB)
			if err != nil {
				return 0, 0, fmt.Errorf("unable to start Stockfish: %w", err)
			}
		}
	}
	evals := map[string]*Evaluation{}
	cloudCalls := 0
	found := 0
	for i, k := range selected {
		if ctx.Err() != nil {
			return i, found, ctx.Err()
		}
		if value, ok := localCache[k]; ok && (source == "local" || source == "hybrid") {
			evals[k] = value
		} else {
			var ev *Evaluation
			if source == "cloud" || source == "hybrid" {
				if source == "cloud" || cloudCalls < cfg.CloudLimit {
					previous := c.NetworkCalls
					raw, e := c.Fetch(ctx, "cloud", "https://lichess.org/api/cloud-eval", map[string]string{"fen": k, "multiPv": "1", "variant": "standard"})
					if e != nil {
						return i, found, e
					}
					cloudCalls += c.NetworkCalls - previous
					var data struct {
						Depth int `json:"depth"`
						PVS   []struct {
							CP   *int `json:"cp"`
							Mate *int `json:"mate"`
						} `json:"pvs"`
					}
					if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &data) == nil && len(data.PVS) > 0 {
						pv := data.PVS[0]
						sideWhite := strings.Fields(k)[1] == "w"
						if pv.CP != nil {
							v := *pv.CP
							if !sideWhite {
								v = -v
							}
							ev = &Evaluation{CP: &v, Depth: data.Depth, Source: "cloud"}
						} else if pv.Mate != nil {
							v := *pv.Mate
							if !sideWhite {
								v = -v
							}
							cp := 100000
							if v < 0 {
								cp = -100000
							}
							ev = &Evaluation{CP: &cp, Mate: &v, Depth: data.Depth, Source: "cloud"}
						}
					}
				}
			}
			if ev == nil && (source == "local" || source == "hybrid") {
				b, _ := ParseFEN(k + " 0 1")
				if len(b.moves()) == 0 {
					cp := 0
					if b.check(b.White) {
						cp = -100000
						if !b.White {
							cp = 100000
						}
					}
					ev = &Evaluation{CP: &cp, Depth: 0, Source: "local"}
				} else if engine != nil {
					var err error
					ev, err = engine.analyze(ctx, k+" 0 1", b.White, cfg.Depth)
					if err != nil {
						logger.Printf("Stockfish evaluation failed: %s", err)
					}
				}
				localCache[k] = ev
				if (i+1)%10 == 0 {
					saveCache()
				}
			}
			evals[k] = ev
		}
		if evals[k] != nil {
			found++
		}
		if (i+1)%10 == 0 || i == len(selected)-1 {
			progress("engine", i+1, len(selected), 55+40*float64(i+1)/math.Max(1, float64(len(selected))), "Stockfish / Lichess Cloud evaluations")
		}
		if (i+1)%40 == 0 {
			logger.Printf("Engine: %d/%d positions, %d evaluations", i+1, len(selected), found)
		}
	}
	for _, n := range r.Nodes {
		n.Eval = evals[n.Key]
	}
	progress("engine", len(selected), len(selected), 95, "Evaluations completed")
	return len(selected), found, nil
}
