package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ExplorerMove struct {
	UCI   string `json:"uci"`
	White int64  `json:"white"`
	Draws int64  `json:"draws"`
	Black int64  `json:"black"`
}
type ExplorerData struct {
	White int64          `json:"white"`
	Draws int64          `json:"draws"`
	Black int64          `json:"black"`
	Moves []ExplorerMove `json:"moves"`
}
type Client struct {
	Cache                    string
	Token                    string
	Delay                    time.Duration
	Offline                  bool
	Last                     time.Time
	NetworkCalls, RateLimits int
	HTTP                     *http.Client
	Logger                   *log.Logger
}

func cacheDirectory() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "repertoire_optimizer")
}
func newClient(cfg Config, token string, lg *log.Logger) *Client {
	p := cacheDirectory()
	_ = os.MkdirAll(p, 0750)
	return &Client{Cache: p, Token: token, Delay: time.Duration(cfg.Delay * float64(time.Second)), Offline: cfg.Offline, HTTP: &http.Client{Timeout: 55 * time.Second}, Logger: lg}
}

// Match the Python application's sorted, spaced JSON cache keys.
func pythonCacheKey(service string, params map[string]string) string {
	keys := []string{}
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := []string{}
	for _, k := range keys {
		key, _ := json.Marshal(k)
		val := params[k]
		if k == "moves" || k == "topGames" || k == "recentGames" || k == "multiPv" {
			if _, err := strconv.Atoi(val); err == nil {
				items = append(items, string(key)+": "+val)
				continue
			}
		}
		value, _ := json.Marshal(val)
		items = append(items, string(key)+": "+string(value))
	}
	payload := "{\"params\": {" + strings.Join(items, ", ") + "}, \"service\": \"" + service + "\"}"
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:])
}
func waitContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (c *Client) Fetch(ctx context.Context, service, endpoint string, params map[string]string) ([]byte, error) {
	name := service + "_" + pythonCacheKey(service, params) + ".json"
	path := filepath.Join(c.Cache, name)
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	if c.Offline {
		return nil, nil
	}
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	full := endpoint + "?" + q.Encode()
	for attempt := 0; attempt < 6; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !c.Last.IsZero() {
			if err := waitContext(ctx, c.Delay-time.Since(c.Last)); err != nil {
				return nil, err
			}
		}
		c.Last = time.Now()
		c.NetworkCalls++
		req, err := http.NewRequestWithContext(ctx, "GET", full, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ChessPrune-Go/0.1")
		req.Header.Set("Accept", "application/json")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			if attempt == 5 {
				return nil, err
			}
			_ = waitContext(ctx, time.Duration((attempt+1)*5)*time.Second)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024))
		res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if res.StatusCode == 404 && service == "cloud" {
			_ = os.WriteFile(path, []byte("null"), 0600)
			return nil, nil
		}
		if res.StatusCode == 429 {
			c.RateLimits++
			retry := 60 * time.Second
			if s := res.Header.Get("Retry-After"); s != "" {
				if v, e := strconv.Atoi(s); e == nil && v > 60 && v < 300 {
					retry = time.Duration(v) * time.Second
				}
			}
			c.Delay = time.Duration(math.Min(float64(15*time.Second), math.Max(float64(c.Delay)+float64(time.Second/2), float64(c.Delay)*1.5)))
			c.Logger.Printf("Lichess HTTP 429 (%s): waiting %s (attempt %d/6)", service, retry, attempt+1)
			if e := waitContext(ctx, retry); e != nil {
				return nil, e
			}
			continue
		}
		if res.StatusCode == 401 || res.StatusCode == 403 {
			return nil, fmt.Errorf("Lichess authentication rejected (HTTP %d)", res.StatusCode)
		}
		if res.StatusCode >= 500 {
			c.Logger.Printf("Lichess HTTP %d (%s)", res.StatusCode, service)
			if e := waitContext(ctx, time.Duration(8+attempt*5)*time.Second); e != nil {
				return nil, e
			}
			continue
		}
		if res.StatusCode >= 400 {
			return nil, fmt.Errorf("Lichess HTTP %d: %s", res.StatusCode, string(body[:min(200, len(body))]))
		}
		if !json.Valid(body) {
			return nil, errors.New("invalid JSON from Lichess")
		}
		tmp := path + ".tmp"
		if e := os.WriteFile(tmp, body, 0600); e == nil {
			_ = os.Rename(tmp, path)
		}
		return body, nil
	}
	return nil, fmt.Errorf("Lichess %s failed after six retries", service)
}
func cnt(e ExplorerMove) *Counts { return &Counts{White: e.White, Draws: e.Draws, Black: e.Black} }
func (r *Repertoire) FetchExplorer(ctx context.Context, c *Client, cfg Config, progress func(string, int, int, float64, string)) (int, error) {
	data := map[string]*ExplorerData{}
	seen := map[int]bool{}
	queued := map[int]bool{}
	type task struct {
		idx    int
		weight int64
	}
	queue := []task{}
	for _, root := range r.Roots {
		queue = append(queue, task{root, math.MaxInt64 / 2})
	}
	queries := 0
	for len(queue) > 0 {
		sort.SliceStable(queue, func(i, j int) bool { return queue[i].weight > queue[j].weight })
		it := queue[0]
		queue = queue[1:]
		n := r.Nodes[it.idx]
		if seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		if _, exists := data[n.Key]; !exists {
			if cfg.MaxQueries > 0 && queries >= cfg.MaxQueries {
				break
			}
			skip := false
			if n.Parent != nil && len(n.Children) == 0 {
				p := r.Nodes[*n.Parent]
				pd := data[p.Key]
				if pd != nil {
					for _, m := range pd.Moves {
						if m.UCI == n.UCI {
							skip = true
							break
						}
					}
				}
			}
			if cfg.QueryMinGames > 0 && n.Depth > 0 && it.weight < int64(cfg.QueryMinGames) {
				skip = true
			}
			if !skip {
				raw, e := c.Fetch(ctx, "explorer", "https://explorer.lichess.org/lichess", map[string]string{"variant": "standard", "fen": n.Key, "speeds": strings.Join(cfg.Speeds, ","), "ratings": strings.Join(cfg.Ratings, ","), "moves": "50", "topGames": "0", "recentGames": "0"})
				if e != nil {
					return queries, e
				}
				var result *ExplorerData
				if len(raw) > 0 && string(raw) != "null" {
					if err := json.Unmarshal(raw, &result); err != nil {
						return queries, err
					}
				}
				data[n.Key] = result
				queries++
				if queries%15 == 0 {
					progress("explorer", queries, len(r.ByKey), 5+50*math.Min(1, float64(queries)/float64(len(r.ByKey))), "Collecting Lichess Opening Explorer statistics")
				}
				if queries%30 == 0 {
					c.Logger.Printf("Explorer: %d / %d unique positions (HTTP calls: %d)", queries, len(r.ByKey), c.NetworkCalls)
				}
			}
		}
		bymove := map[string]int64{}
		if d := data[n.Key]; d != nil {
			for _, m := range d.Moves {
				bymove[m.UCI] = m.White + m.Draws + m.Black
			}
		}
		for _, child := range n.Children {
			if !queued[child] {
				queued[child] = true
				queue = append(queue, task{child, bymove[r.Nodes[child].UCI]})
			}
		}
	}
	for _, n := range r.Nodes {
		var counts *Counts
		if n.Parent == nil {
			if d := data[n.Key]; d != nil {
				counts = &Counts{White: d.White, Draws: d.Draws, Black: d.Black}
			}
		} else {
			p := r.Nodes[*n.Parent]
			if d := data[p.Key]; d != nil {
				for _, m := range d.Moves {
					if m.UCI == n.UCI {
						counts = cnt(m)
						n.EdgeKnown = true
						break
					}
				}
			}
			if counts == nil {
				if d := data[n.Key]; d != nil {
					counts = &Counts{White: d.White, Draws: d.Draws, Black: d.Black}
				}
			}
		}
		n.Stats = counts
		n.Stats.addScore(cfg.Side)
	}
	progress("explorer", queries, len(r.ByKey), 55, "Lichess statistics ready")
	return queries, nil
}
