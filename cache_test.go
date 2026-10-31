package main

import "testing"

func TestPythonExplorerCacheCompatibility(t *testing.T) {
	params := map[string]string{"variant": "standard", "fen": "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -", "speeds": "blitz,rapid", "ratings": "1800,2000", "moves": "50", "topGames": "0", "recentGames": "0"}
	got := pythonCacheKey("explorer", params)
	want := "54bd1298dca0b2b0c3eba121888803d107b1936a269694ecab69a70e0a4ad05a"
	if got != want {
		t.Fatalf("Python cache mismatch: got %s want %s", got, want)
	}
}
