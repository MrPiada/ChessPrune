package main

import (
	"math"
	"sort"
)

type SelectResult struct {
	Kept              map[int]bool
	Leaves            []int
	PerChapter        []int
	Available, Actual int
}
type LineCandidate struct {
	Path          []int
	Chapter, Leaf int
}

func SelectTarget(r *Repertoire, cfg Config) SelectResult {
	prob := map[int]float64{}
	cands := make([][]LineCandidate, len(r.Roots))
	var walk func(int, []int, float64, int)
	walk = func(idx int, path []int, p float64, ch int) {
		n := r.Nodes[idx]
		path = append(append([]int{}, path...), idx)
		prob[idx] = p
		if len(n.Children) == 0 {
			cands[ch] = append(cands[ch], LineCandidate{path, ch, idx})
			return
		}
		if n.Turn == cfg.Side {
			for _, id := range n.Children[:min(len(n.Children), max(1, cfg.RepertoireOptions))] {
				walk(id, path, p, ch)
			}
		} else {
			weights := []float64{}
			sum := 0.0
			for _, id := range n.Children {
				w := float64(r.Nodes[id].Stats.total())
				if w <= 0 {
					w = 1
				}
				weights = append(weights, w)
				sum += w
			}
			for j, id := range n.Children {
				walk(id, path, p*weights[j]/sum, ch)
			}
		}
	}
	for ch, root := range r.Roots {
		walk(root, nil, 1, ch)
	}
	available := 0
	for _, group := range cands {
		available += len(group)
	}
	cap := min(cfg.TargetLines, available)
	if cap < 0 {
		cap = 0
	}
	selected := map[int]bool{}
	taken := map[int]bool{}
	leaves := []int{}
	counts := make([]int, len(r.Roots))
	marginal := func(c LineCandidate) float64 {
		newnodes := []*Node{}
		for _, id := range c.Path {
			if !selected[id] {
				newnodes = append(newnodes, r.Nodes[id])
			}
		}
		if len(newnodes) == 0 {
			return -1
		}
		main := 0.0
		for _, n := range newnodes {
			main += prob[n.ID] * (.60 + .40*math.Max(0, math.Min(100, n.Importance))/100)
		}
		main /= float64(len(newnodes))
		main *= 1 + .10*math.Log2(1+float64(len(newnodes)))
		firstN := float64(newnodes[0].Stats.total())
		rarity := .30 + .70*math.Min(1, firstN/math.Max(1, float64(cfg.MinGames)))
		bonus := 0.0
		for _, n := range newnodes {
			if n.PracticalTrap != nil || (n.Trap != nil && n.Trap["confirmed"] == true) {
				nn := float64(n.Stats.total())
				confidence := math.Sqrt(nn / (nn + 100))
				reach := math.Max(.15, math.Pow(prob[n.ID], .30))
				scale := .65
				if n.PracticalTrap != nil {
					scale = 1
				}
				bonus += .08 * confidence * reach * scale
			}
		}
		fair := 1 / (1 + .12*float64(counts[c.Chapter]))
		return (main*rarity + cfg.TrapBoost*bonus) * fair
	}
	choose := func(chapter int) bool {
		best := -1.0
		var selectedLine LineCandidate
		found := false
		for ch, group := range cands {
			if chapter >= 0 && chapter != ch {
				continue
			}
			for _, c := range group {
				if taken[c.Leaf] {
					continue
				}
				v := marginal(c)
				if !found || v > best+1e-12 || (math.Abs(v-best) < 1e-12 && c.Leaf < selectedLine.Leaf) {
					found = true
					best = v
					selectedLine = c
				}
			}
		}
		if !found {
			return false
		}
		for _, id := range selectedLine.Path {
			selected[id] = true
		}
		taken[selectedLine.Leaf] = true
		leaves = append(leaves, selectedLine.Leaf)
		counts[selectedLine.Chapter]++
		return true
	}
	for i := 0; i < cfg.MinChapterLines; i++ {
		for ch := range r.Roots {
			if len(taken) >= cap {
				break
			}
			if counts[ch] < len(cands[ch]) {
				choose(ch)
			}
		}
	}
	for len(taken) < cap {
		if !choose(-1) {
			break
		}
	}
	return SelectResult{selected, leaves, counts, available, cap}
}
func CoveragePrune(r *Repertoire, cfg Config) {
	var visit func(int)
	visit = func(idx int) {
		n := r.Nodes[idx]
		n.Keep = true
		if len(n.Children) == 0 {
			return
		}
		chosen := []int{}
		if n.Turn == cfg.Side {
			chosen = append(chosen, n.Children[:min(len(n.Children), max(1, cfg.RepertoireOptions))]...)
		} else {
			known, unknown := []int{}, []int{}
			for _, id := range n.Children {
				c := r.Nodes[id]
				if c.EdgeKnown && c.Stats != nil {
					known = append(known, id)
				} else {
					unknown = append(unknown, id)
				}
			}
			sort.SliceStable(known, func(i, j int) bool {
				a, b := r.Nodes[known[i]], r.Nodes[known[j]]
				if a.Importance != b.Importance {
					return a.Importance > b.Importance
				}
				return a.Stats.total() > b.Stats.total()
			})
			covered := int64(0)
			parentN := n.Stats.total()
			for _, id := range known {
				c := r.Nodes[id]
				if len(chosen) > 0 && parentN > 0 && float64(covered)/float64(parentN) >= cfg.Coverage {
					break
				}
				if len(chosen) > 0 && (c.Stats.total() < int64(cfg.MinGames) || c.Importance < cfg.MinImportance) {
					continue
				}
				chosen = append(chosen, id)
				covered += c.Stats.total()
			}
			chosen = append(chosen, unknown...)
			if cfg.KeepTraps {
				for _, id := range n.Children {
					c := r.Nodes[id]
					if c.Trap != nil && c.Trap["confirmed"] == true && c.Stats.total() >= int64(cfg.TrapMinGames) {
						chosen = append(chosen, id)
					}
				}
			}
			if len(chosen) == 0 {
				chosen = append(chosen, n.Children[0])
			}
		}
		seen := map[int]bool{}
		for _, id := range chosen {
			if !seen[id] {
				seen[id] = true
				visit(id)
			}
		}
	}
	for _, root := range r.Roots {
		visit(root)
	}
}
func Select(r *Repertoire, cfg Config) SelectResult {
	for _, n := range r.Nodes {
		n.Keep = false
	}
	if cfg.FilterMode == "target" {
		s := SelectTarget(r, cfg)
		for id := range s.Kept {
			r.Nodes[id].Keep = true
		}
		return s
	}
	CoveragePrune(r, cfg)
	return SelectResult{}
}
