package main

import (
	"fmt"
	"strings"
)

type Config struct {
	Side              string   `json:"side"`
	Mode              string   `json:"mode"`
	Ratings           []string `json:"ratings"`
	Speeds            []string `json:"speeds"`
	FilterMode        string   `json:"filter_mode"`
	EvalSource        string   `json:"eval_source"`
	TargetLines       int      `json:"target_lines"`
	MinChapterLines   int      `json:"min_chapter_lines"`
	RepertoireOptions int      `json:"repertoire_options"`
	MinGames          int      `json:"min_games"`
	MaxEvals          int      `json:"max_evals"`
	MaxQueries        int      `json:"max_queries,omitempty"`
	SampleLines       int      `json:"sample_lines"`
	Depth             int      `json:"depth"`
	Threads           int      `json:"threads"`
	HashMB            int      `json:"hash_mb"`
	CloudLimit        int      `json:"cloud_limit"`
	Coverage          float64  `json:"coverage"`
	TrapBoost         float64  `json:"trap_boost"`
	MinImportance     float64  `json:"min_importance"`
	Delay             float64  `json:"delay"`
	Offline           bool     `json:"offline"`
	Annotate          bool     `json:"annotate"`
	KeepTraps         bool     `json:"keep_traps"`
	Engine            string   `json:"engine"`
	TrapDrop          int      `json:"trap_drop"`
	TrapAdvantage     int      `json:"trap_advantage"`
	TrapMinGames      int      `json:"trap_min_games"`
	PracticalMinGames int      `json:"practical_min_games"`
	PracticalMinScore float64  `json:"practical_min_score"`
	PracticalMaxAbsCP int      `json:"practical_max_abs_cp"`
	PracticalMinGap   float64  `json:"practical_min_gap"`
	QueryMinGames     int      `json:"query_min_games"`
}

func DefaultConfig() Config {
	return Config{Side: "black", Mode: "test", Ratings: []string{"1800", "2000"}, Speeds: []string{"blitz", "rapid"}, FilterMode: "target", EvalSource: "auto", TargetLines: 50, MinChapterLines: 2, RepertoireOptions: 1, MinGames: 20, MaxEvals: 80, SampleLines: 16, Depth: 14, Threads: 2, HashMB: 128, CloudLimit: 50, Coverage: .7, TrapBoost: 1, Delay: 4, KeepTraps: true, TrapDrop: 100, TrapAdvantage: 80, TrapMinGames: 10, PracticalMinGames: 40, PracticalMinScore: .66, PracticalMaxAbsCP: 60, PracticalMinGap: .10}
}
func (c Config) Validate() error {
	if c.Side != "white" && c.Side != "black" {
		return fmt.Errorf("choose White or Black")
	}
	if c.Mode != "test" && c.Mode != "full" {
		return fmt.Errorf("invalid analysis mode")
	}
	if c.FilterMode != "target" && c.FilterMode != "coverage" {
		return fmt.Errorf("invalid selection strategy")
	}
	if !strings.Contains("|local|cloud|hybrid|auto|none|", "|"+c.EvalSource+"|") {
		return fmt.Errorf("invalid evaluation source")
	}
	if len(c.Ratings) == 0 || len(c.Speeds) == 0 {
		return fmt.Errorf("select rating bands and time controls")
	}
	for _, r := range c.Ratings {
		if !strings.Contains("|0|1000|1200|1400|1600|1800|2000|2200|2500|", "|"+r+"|") {
			return fmt.Errorf("invalid rating %s", r)
		}
	}
	for _, s := range c.Speeds {
		if !strings.Contains("|bullet|blitz|rapid|classical|correspondence|", "|"+s+"|") {
			return fmt.Errorf("invalid speed %s", s)
		}
	}
	if c.TargetLines < 1 || c.TargetLines > 3000 || c.MinChapterLines < 0 || c.MinChapterLines > 100 || c.RepertoireOptions < 1 || c.RepertoireOptions > 10 {
		return fmt.Errorf("invalid line-selection parameters")
	}
	if c.Depth < 1 || c.Depth > 40 || c.Threads < 1 || c.Threads > 64 || c.HashMB < 16 || c.HashMB > 16384 {
		return fmt.Errorf("invalid Stockfish configuration")
	}
	if c.MinGames < 0 || c.SampleLines < 1 || c.MaxEvals < 0 || c.CloudLimit < 0 || c.Delay < .5 || c.Delay > 120 || c.Coverage < .01 || c.Coverage > 1 || c.TrapBoost < 0 || c.TrapBoost > 3 || c.MinImportance < 0 || c.MinImportance > 100 {
		return fmt.Errorf("invalid analysis thresholds")
	}
	return nil
}
