package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Report struct {
	Config         map[string]any      `json:"config"`
	Summary        map[string]any      `json:"summary"`
	Nodes          []*Node             `json:"nodes"`
	Roots          []int               `json:"roots"`
	Headers        []map[string]string `json:"headers"`
	Positions      []map[string]any    `json:"positions"`
	Traps          []map[string]any    `json:"traps"`
	Leaves         []map[string]any    `json:"leaves"`
	PracticalTraps []map[string]any    `json:"practical_traps"`
}

func (r *Repertoire) DetectTraps(cfg Config) []map[string]any {
	out := []map[string]any{}
	sign := 1
	if cfg.Side == "black" {
		sign = -1
	}
	for _, n := range r.Nodes {
		if n.Parent == nil {
			continue
		}
		parent := r.Nodes[*n.Parent]
		if parent.Turn == cfg.Side || parent.Eval == nil || n.Eval == nil || parent.Eval.CP == nil || n.Eval.CP == nil {
			continue
		}
		delta := sign * (*n.Eval.CP - *parent.Eval.CP)
		if delta < cfg.TrapDrop || sign*(*n.Eval.CP) < cfg.TrapAdvantage {
			continue
		}
		confirmed := false
		reply := "—"
		for _, id := range n.Children[:min(len(n.Children), cfg.RepertoireOptions)] {
			c := r.Nodes[id]
			if reply == "—" {
				reply = c.SAN
			}
			if c.Eval != nil && c.Eval.CP != nil && sign*(*c.Eval.CP) >= cfg.TrapAdvantage {
				confirmed = true
				reply = c.SAN
				break
			}
		}
		status := "unverified"
		if confirmed {
			status = "confirmed"
		}
		t := map[string]any{"id": n.ID, "parent": *n.Parent, "delta": delta, "cp": *n.Eval.CP, "n": n.Stats.total(), "score": n.Stats.Score, "san": n.SAN, "status": status, "confirmed": confirmed, "response": reply}
		n.Trap = t
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a["confirmed"].(bool) != b["confirmed"].(bool) {
			return a["confirmed"].(bool)
		}
		return a["n"].(int64) > b["n"].(int64)
	})
	return out
}
func (r *Repertoire) PracticalTraps(cfg Config) []map[string]any {
	out := []map[string]any{}
	for _, n := range r.Nodes {
		if n.Eval == nil || n.Eval.CP == nil || n.Stats == nil || n.Depth < 3 || n.Turn == cfg.Side {
			continue
		}
		count := n.Stats.total()
		cp := *n.Eval.CP
		if abs(cp) > cfg.PracticalMaxAbsCP || count < int64(cfg.PracticalMinGames) || n.Stats.Score == nil {
			continue
		}
		sign := 1
		if cfg.Side == "black" {
			sign = -1
		}
		signedCP := sign * cp
		expected := 1 / (1 + math.Exp(math.Max(-50, math.Min(50, -float64(signedCP)/180))))
		wins := float64(n.Stats.White)
		if cfg.Side == "black" {
			wins = float64(n.Stats.Black)
		}
		shrunk := (wins + float64(n.Stats.Draws)*.5 + 10) / float64(count+20)
		gap := shrunk - expected
		if *n.Stats.Score < cfg.PracticalMinScore || gap < cfg.PracticalMinGap {
			continue
		}
		trap := map[string]any{"id": n.ID, "n": count, "score": *n.Stats.Score, "expected": expected, "gap": gap, "eval_cp": cp, "label": "Practical trap / deceptive position"}
		n.PracticalTrap = trap
		out = append(out, trap)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return float64(out[i]["n"].(int64))*out[i]["gap"].(float64) > float64(out[j]["n"].(int64))*out[j]["gap"].(float64)
	})
	return out
}
func (r *Repertoire) Importance(cfg Config) {
	chapterN := map[int]int64{}
	for _, n := range r.Nodes {
		if n.Depth >= 3 {
			chapterN[n.Chapter] = max(chapterN[n.Chapter], n.Stats.total())
		}
	}
	sign := 1
	if cfg.Side == "black" {
		sign = -1
	}
	for _, n := range r.Nodes {
		count := n.Stats.total()
		frequency := 0.0
		if chapterN[n.Chapter] > 0 {
			frequency = math.Log1p(float64(count)) / math.Log1p(float64(chapterN[n.Chapter]))
		}
		practical := .5
		if n.Stats != nil && n.Stats.Score != nil {
			practical = (*n.Stats.Score*float64(count) + .5*40) / float64(count+40)
		}
		objective := .5
		if n.Eval != nil && n.Eval.CP != nil {
			objective = (1 + math.Tanh(float64(sign**n.Eval.CP)/240)) / 2
		}
		bonus := 0.0
		if n.PracticalTrap != nil {
			bonus = .10
		}
		n.Importance = math.Round(10000*math.Min(1, .55*frequency+.25*practical+.20*objective+bonus)) / 100
	}
}
func (r *Repertoire) MakeReport(cfg Config, queries, attempted, found int, traps, practical []map[string]any) *Report {
	r.Lines()
	positions := []map[string]any{}
	for _, ids := range r.ByKey {
		rep := r.Nodes[ids[0]]
		nmax := int64(0)
		for _, id := range ids {
			n := r.Nodes[id]
			if n.Depth < rep.Depth {
				rep = n
			}
			nmax = max(nmax, n.Stats.total())
		}
		if rep.Depth < 3 {
			continue
		}
		var repScore any
		if rep.Stats != nil {
			repScore = rep.Stats.Score
		}
		positions = append(positions, map[string]any{"id": rep.ID, "n": nmax, "score": repScore, "chapter": rep.ChapterName})
	}
	sort.SliceStable(positions, func(i, j int) bool { return positions[i]["n"].(int64) > positions[j]["n"].(int64) })
	leaves := []map[string]any{}
	for _, n := range r.Nodes {
		if len(n.Children) == 0 {
			var score any
			if n.Stats != nil {
				score = n.Stats.Score
			}
			leaves = append(leaves, map[string]any{"id": n.ID, "n": n.Stats.total(), "score": score, "kept": n.Keep})
		}
	}
	sort.SliceStable(leaves, func(i, j int) bool { return leaves[i]["n"].(int64) > leaves[j]["n"].(int64) })
	kept, trapsConfirmed := 0, 0
	for _, n := range r.Nodes {
		if n.Keep {
			kept++
		}
	}
	for _, t := range traps {
		if t["confirmed"] == true {
			trapsConfirmed++
		}
	}
	config := map[string]any{"side": cfg.Side, "repertoire_options": cfg.RepertoireOptions, "filter_mode": cfg.FilterMode, "target_lines": cfg.TargetLines, "min_chapter_lines": cfg.MinChapterLines, "trap_boost": cfg.TrapBoost, "ratings": strings.Join(cfg.Ratings, ","), "speeds": strings.Join(cfg.Speeds, ","), "coverage": cfg.Coverage, "min_games": cfg.MinGames, "black_options": cfg.RepertoireOptions, "min_importance": cfg.MinImportance, "keep_traps": cfg.KeepTraps, "trap_min_games": cfg.TrapMinGames, "eval_source": cfg.EvalSource, "dry_run": cfg.Mode == "test", "sampled_lines": r.SampledLines, "available_lines": r.AvailableLines, "created": time.Now().UTC().Format("2006-01-02 15:04 UTC")}
	return &Report{config, map[string]any{"chapters": len(r.Roots), "nodes": len(r.Nodes), "positions": len(r.ByKey), "queried": queries, "eval_attempted": attempted, "eval_found": found, "kept": kept, "traps": trapsConfirmed}, r.Nodes, r.Roots, r.Headers, positions, traps, leaves, practical}
}
func WriteDashboard(report *Report, path string) error {
	data, e := json.Marshal(report)
	if e != nil {
		return e
	}
	icon, e := embedded.ReadFile("assets/ChessPrune_Icon.png")
	if e != nil {
		return e
	}
	tpl, e := embedded.ReadFile("dashboard_template.html")
	if e != nil {
		return e
	}
	html := bytes.Replace(tpl, []byte("/*__EMBED_DATA__*/"), append([]byte("const DATA = "), append(data, []byte(";")...)...), 1)
	html = bytes.ReplaceAll(html, []byte("__CHESSPRUNE_ICON__"), []byte("data:image/png;base64,"+base64.StdEncoding.EncodeToString(icon)))
	return os.WriteFile(path, html, 0644)
}
func writeComment(s string) string {
	s = strings.NewReplacer("{", " ", "}", " ", "\r", " ", "\n", " ").Replace(s)
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return "{ " + strings.TrimSpace(s) + " } "
}
func (r *Repertoire) exportNode(id int, keep map[int]bool) string {
	n := r.Nodes[id]
	if n.Parent == nil {
		return ""
	}
	p := r.Nodes[*n.Parent]
	prefix := fmt.Sprintf("%d. ", p.board.Full)
	if p.Turn == "black" {
		prefix = fmt.Sprintf("%d... ", p.board.Full)
	}
	out := prefix + writeComment(n.StartingComment) + n.SAN + " "
	for _, nag := range n.NAGs {
		out += fmt.Sprintf("$%d ", nag)
	}
	out += writeComment(n.Comment)
	return out
}
func (r *Repertoire) continuation(id int, keep map[int]bool) string {
	kids := []int{}
	for _, c := range r.Nodes[id].Children {
		if keep[c] {
			kids = append(kids, c)
		}
	}
	if len(kids) == 0 {
		return ""
	}
	text := r.exportNode(kids[0], keep)
	for _, c := range kids[1:] {
		text += "( " + r.exportNode(c, keep) + r.continuation(c, keep) + ") "
	}
	return text + r.continuation(kids[0], keep)
}
func (r *Repertoire) ExportPGN(path string, keep map[int]bool) error {
	var out strings.Builder
	for _, root := range r.Roots {
		if !keep[root] {
			continue
		}
		headers := r.Headers[r.Nodes[root].Chapter]
		priority := []string{"Event", "Site", "Date", "Round", "White", "Black", "Result", "ChapterName", "FEN", "SetUp", "Variant"}
		already := map[string]bool{}
		for _, k := range priority {
			if v, ok := headers[k]; ok {
				fmt.Fprintf(&out, "[%s %q]\n", k, v)
				already[k] = true
			}
		}
		additional := []string{}
		for k := range headers {
			if !already[k] {
				additional = append(additional, k)
			}
		}
		sort.Strings(additional)
		for _, k := range additional {
			fmt.Fprintf(&out, "[%s %q]\n", k, headers[k])
		}
		out.WriteString("\n")
		out.WriteString(writeComment(r.Nodes[root].Comment))
		out.WriteString(r.continuation(root, keep))
		result := headers["Result"]
		if result == "" {
			result = "*"
		}
		out.WriteString(" " + result + "\n\n")
	}
	return os.WriteFile(path, []byte(out.String()), 0644)
}
func (r *Repertoire) ExportFiltered(path string) error {
	kept := map[int]bool{}
	for _, n := range r.Nodes {
		if n.Keep {
			kept[n.ID] = true
		}
	}
	return r.ExportPGN(path, kept)
}
func (r *Repertoire) ExportTarget(path string, cfg Config) (SelectResult, error) {
	result := SelectTarget(r, cfg)
	return result, r.ExportPGN(path, result.Kept)
}
func ExecuteAnalysis(ctx context.Context, content string, cfg Config, token, out string, lg *log.Logger, progress func(string, int, int, float64, string)) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if (cfg.EvalSource == "local" || cfg.EvalSource == "hybrid") && findEngine(cfg.Engine) == "" {
		return fmt.Errorf("Stockfish not found: choose Auto / Cloud / None or specify its path")
	}
	progress("explorer", 0, 0, 5, "Parsing PGN")
	r, err := ParsePGN(content)
	if err != nil {
		return err
	}
	if cfg.Mode == "test" {
		r.Sample(cfg.SampleLines)
	}
	lg.Printf("PGN parsed: %d chapters, %d nodes, %d distinct positions", len(r.Roots), len(r.Nodes), len(r.ByKey))
	c := newClient(cfg, token, lg)
	queries, e := r.FetchExplorer(ctx, c, cfg, progress)
	if e != nil {
		return e
	}
	attempted, found, e := r.Evaluate(ctx, c, cfg, progress, lg)
	if e != nil {
		return e
	}
	traps := r.DetectTraps(cfg)
	practical := r.PracticalTraps(cfg)
	r.Importance(cfg)
	selection := Select(r, cfg)
	progress("finalize", 0, 1, 96, "Generating HTML dashboard and PGN files")
	report := r.MakeReport(cfg, queries, attempted, found, traps, practical)
	if err = os.MkdirAll(out, 0750); err != nil {
		return err
	}
	blob, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		return e
	}
	if err = os.WriteFile(filepath.Join(out, "data.json"), blob, 0644); err != nil {
		return err
	}
	if err = WriteDashboard(report, filepath.Join(out, "dashboard.html")); err != nil {
		return err
	}
	if err = r.ExportFiltered(filepath.Join(out, "filtered_repertoire.pgn")); err != nil {
		return err
	}
	if cfg.FilterMode == "target" {
		path := filepath.Join(out, fmt.Sprintf("target_repertoire_%d_lines.pgn", len(selection.Leaves)))
		if _, err = r.ExportTarget(path, cfg); err != nil {
			return err
		}
	}
	progress("finalize", 1, 1, 100, "Analysis complete")
	lg.Printf("Completed: %d nodes, %d kept; %d Lichess queries; %d/%d evaluations; %d practical traps", len(r.Nodes), report.Summary["kept"], queries, found, attempted, len(practical))
	return nil
}
