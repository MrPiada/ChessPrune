package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Counts struct {
	White int64    `json:"white"`
	Draws int64    `json:"draws"`
	Black int64    `json:"black"`
	Score *float64 `json:"score,omitempty"`
	N     int64    `json:"n"`
}

func (c *Counts) total() int64 {
	if c == nil {
		return 0
	}
	return c.White + c.Draws + c.Black
}
func (c *Counts) addScore(side string) {
	if c == nil {
		return
	}
	c.N = c.total()
	if c.N == 0 {
		return
	}
	wins := c.Black
	if side == "white" {
		wins = c.White
	}
	v := (float64(wins) + float64(c.Draws)*0.5) / float64(c.N)
	c.Score = &v
}

type Evaluation struct {
	CP     *int   `json:"cp"`
	Mate   *int   `json:"mate"`
	Depth  int    `json:"depth"`
	Source string `json:"source"`
}
type Node struct {
	ID              int            `json:"id"`
	Parent          *int           `json:"parent"`
	Chapter         int            `json:"chapter"`
	ChapterName     string         `json:"chapter_name"`
	Depth           int            `json:"depth"`
	SAN             string         `json:"san"`
	UCI             string         `json:"uci"`
	Mainline        bool           `json:"mainline"`
	FEN             string         `json:"fen"`
	Key             string         `json:"key"`
	Turn            string         `json:"turn"`
	Comment         string         `json:"comment"`
	StartingComment string         `json:"starting_comment"`
	NAGs            []int          `json:"nags"`
	Children        []int          `json:"children"`
	Stats           *Counts        `json:"stats"`
	Eval            *Evaluation    `json:"eval"`
	Trap            map[string]any `json:"trap"`
	PracticalTrap   map[string]any `json:"practical_trap"`
	Importance      float64        `json:"importance"`
	Keep            bool           `json:"keep"`
	EdgeKnown       bool           `json:"edge_known,omitempty"`
	Line            string         `json:"line"`
	board           Board
	move            Move
}
type Repertoire struct {
	Nodes            []*Node
	Roots            []int
	Headers          []map[string]string
	AvailableLines   int
	SampledLines     int
	OriginalChapters int
	ByKey            map[string][]int
}

func (r *Repertoire) add(parent *Node, chapter int, title string, b Board, m Move, san string) *Node {
	id := len(r.Nodes)
	depth := 0
	var pid *int
	main := true
	if parent != nil {
		depth = parent.Depth + 1
		v := parent.ID
		pid = &v
		main = len(parent.Children) == 0
	}
	turn := "black"
	if b.White {
		turn = "white"
	}
	n := &Node{ID: id, Parent: pid, Chapter: chapter, ChapterName: title, Depth: depth, SAN: san, Mainline: main, FEN: b.FEN(), Key: b.Key(), Turn: turn, NAGs: []int{}, Children: []int{}, board: b, move: m}
	if parent != nil {
		n.UCI = sq(m.From) + sq(m.To)
		if m.Promo != 0 {
			n.UCI += string(m.Promo)
		}
		parent.Children = append(parent.Children, id)
	}
	r.Nodes = append(r.Nodes, n)
	r.ByKey[n.Key] = append(r.ByKey[n.Key], id)
	return n
}

type ptoken struct{ val, typ string }

func scanPGN(s string) ([]ptoken, error) {
	out := []ptoken{}
	for i := 0; i < len(s); {
		switch s[i] {
		case ' ', '\n', '\r', '\t':
			i++
			continue
		case '[':
			j := i + 1
			quoted, escape := false, false
			for j < len(s) {
				c := s[j]
				if escape {
					escape = false
				} else if c == '\\' {
					escape = true
				} else if c == '"' {
					quoted = !quoted
				} else if c == ']' && !quoted {
					break
				}
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated PGN tag")
			}
			out = append(out, ptoken{s[i : j+1], "tag"})
			i = j + 1
		case '{':
			j := i + 1
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated PGN comment")
			}
			out = append(out, ptoken{strings.TrimSpace(s[i+1 : j]), "comment"})
			i = j + 1
		case ';':
			j := i + 1
			for j < len(s) && s[j] != '\n' {
				j++
			}
			out = append(out, ptoken{strings.TrimSpace(s[i+1 : j]), "comment"})
			i = j
		case '(', ')':
			out = append(out, ptoken{s[i : i+1], "paren"})
			i++
		default:
			j := i + 1
			for j < len(s) && !strings.ContainsRune(" \t\n\r{}();[", rune(s[j])) {
				j++
			}
			out = append(out, ptoken{s[i:j], "word"})
			i = j
		}
	}
	return out, nil
}

var tagRX = regexp.MustCompile(`^\[([A-Za-z0-9_]+)\s+"((?:\\.|[^"\\])*)"\]$`)
var numberRX = regexp.MustCompile(`^\d+\.(?:\.{2})?`)
var nagChars = map[string]int{"!": 1, "?": 2, "!!": 3, "??": 4, "!?": 5, "?!": 6}

func ParsePGN(content string) (*Repertoire, error) {
	ts, err := scanPGN(strings.TrimPrefix(content, "\ufeff"))
	if err != nil {
		return nil, err
	}
	r := &Repertoire{ByKey: map[string][]int{}}
	var root, cur *Node
	headers := map[string]string{}
	stack := []*Node{}
	moveSeen := false
	pendingStart := ""
	start := func() error {
		if root != nil {
			return nil
		}
		fen := initialFEN
		if headers["FEN"] != "" {
			fen = headers["FEN"]
		}
		b, e := ParseFEN(fen)
		if e != nil {
			return e
		}
		chapter := len(r.Roots)
		title := headers["ChapterName"]
		if title == "" {
			title = headers["Event"]
		}
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chapter+1)
		}
		root = r.add(nil, chapter, title, b, Move{}, "")
		r.Roots = append(r.Roots, root.ID)
		r.Headers = append(r.Headers, headers)
		cur = root
		return nil
	}
	finish := func() {
		root = nil
		cur = nil
		headers = map[string]string{}
		stack = nil
		moveSeen = false
		pendingStart = ""
	}
	for _, t := range ts {
		if t.typ == "tag" {
			if root != nil && moveSeen {
				finish()
			}
			if root != nil && !moveSeen {
				return nil, fmt.Errorf("PGN headers after chapter start")
			}
			m := tagRX.FindStringSubmatch(t.val)
			if m == nil {
				return nil, fmt.Errorf("invalid PGN tag: %s", t.val)
			}
			s := strings.ReplaceAll(strings.ReplaceAll(m[2], `\"`, `"`), `\\`, `\`)
			headers[m[1]] = s
			continue
		}
		if root == nil {
			if err := start(); err != nil {
				return nil, err
			}
		}
		if t.typ == "comment" {
			if len(stack) > 0 && cur == stack[len(stack)-1].ParentNode(r) {
				pendingStart = t.val
			} else if cur != nil {
				if cur.Comment != "" {
					cur.Comment += "\n"
				}
				cur.Comment += t.val
			}
			continue
		}
		if t.typ == "paren" {
			if t.val == "(" {
				if cur == nil || cur.Parent == nil {
					return nil, fmt.Errorf("PGN variation missing parent")
				}
				stack = append(stack, cur)
				cur = r.Nodes[*cur.Parent]
			} else {
				if len(stack) == 0 {
					return nil, fmt.Errorf("PGN unmatched close parenthesis")
				}
				cur = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			continue
		}
		s := strings.TrimSpace(t.val)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "$") {
			if cur != nil && cur.Parent != nil {
				if n, e := strconv.Atoi(strings.TrimPrefix(s, "$")); e == nil {
					cur.NAGs = append(cur.NAGs, n)
				}
			}
			continue
		}
		if n, ok := nagChars[s]; ok {
			if cur != nil && cur.Parent != nil {
				cur.NAGs = append(cur.NAGs, n)
			}
			continue
		}
		for numberRX.MatchString(s) {
			s = numberRX.ReplaceAllString(s, "")
		}
		if s == "" || s == "..." {
			continue
		}
		if s == "*" || s == "1-0" || s == "0-1" || s == "1/2-1/2" {
			if len(stack) == 0 {
				finish()
			}
			continue
		}
		if strings.EqualFold(headers["Variant"], "Chess960") || (headers["Variant"] != "" && !strings.EqualFold(headers["Variant"], "Standard") && !strings.EqualFold(headers["Variant"], "Chess")) {
			return nil, fmt.Errorf("only standard chess supported; got variant %s", headers["Variant"])
		}
		annotations := ""
		for len(s) > 0 && strings.ContainsAny(string(s[len(s)-1]), "!?") {
			annotations = string(s[len(s)-1]) + annotations
			s = s[:len(s)-1]
		}
		m, e := cur.board.ParseSAN(s)
		if e != nil {
			return nil, fmt.Errorf("chapter %d move %q: %w", len(r.Roots), s, e)
		}
		san := cur.board.SAN(m)
		next := cur.board.Apply(m)
		child := r.add(cur, cur.Chapter, cur.ChapterName, next, m, san)
		child.StartingComment = pendingStart
		pendingStart = ""
		if a, ok := nagChars[annotations]; ok {
			child.NAGs = append(child.NAGs, a)
		}
		cur = child
		moveSeen = true
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("unterminated PGN variation")
	}
	if len(r.Roots) == 0 {
		return nil, fmt.Errorf("empty PGN")
	}
	r.OriginalChapters = len(r.Roots)
	return r, nil
}
func (n *Node) ParentNode(r *Repertoire) *Node {
	if n == nil || n.Parent == nil {
		return nil
	}
	return r.Nodes[*n.Parent]
}
func (r *Repertoire) Sample(limit int) {
	if limit <= 0 {
		return
	}
	type path struct {
		ids     []int
		chapter int
		order   []int
	}
	groups := make([][]path, len(r.Roots))
	var walk func(int, []int, []int, int)
	walk = func(idx int, ids, order []int, ch int) {
		n := r.Nodes[idx]
		ids = append(append([]int{}, ids...), idx)
		if len(n.Children) == 0 {
			if len(ids) > 1 {
				groups[ch] = append(groups[ch], path{ids, ch, order})
			}
			return
		}
		for i, c := range n.Children {
			walk(c, ids, append(append([]int{}, order...), i), ch)
		}
	}
	for ch, root := range r.Roots {
		walk(root, nil, nil, ch)
	}
	for ch := range groups {
		sort.SliceStable(groups[ch], func(i, j int) bool {
			a, b := groups[ch][i].order, groups[ch][j].order
			fa, fb := 999, 999
			for k, v := range a {
				if v != 0 {
					fa = k
					break
				}
			}
			for k, v := range b {
				if v != 0 {
					fb = k
					break
				}
			}
			if fa != fb {
				return fa < fb
			}
			return groups[ch][i].ids[len(groups[ch][i].ids)-1] < groups[ch][j].ids[len(groups[ch][j].ids)-1]
		})
	}
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	r.AvailableLines = total
	kept := map[int]bool{}
	indices := make([]int, len(groups))
	remaining := limit
	if remaining > total {
		remaining = total
	}
	r.SampledLines = remaining
	for remaining > 0 {
		progress := false
		for ch, g := range groups {
			if indices[ch] >= len(g) || remaining == 0 {
				continue
			}
			for _, id := range g[indices[ch]].ids {
				kept[id] = true
			}
			indices[ch]++
			remaining--
			progress = true
		}
		if !progress {
			break
		}
	}
	old := r.Nodes
	mapping := map[int]int{}
	nr := &Repertoire{ByKey: map[string][]int{}, OriginalChapters: r.OriginalChapters, AvailableLines: r.AvailableLines, SampledLines: r.SampledLines}
	for _, n := range old {
		if !kept[n.ID] {
			continue
		}
		copyNode := *n
		copyNode.ID = len(nr.Nodes)
		copyNode.Children = []int{}
		copyNode.NAGs = append([]int{}, n.NAGs...)
		if n.Parent != nil {
			v := mapping[*n.Parent]
			copyNode.Parent = &v
		}
		mapping[n.ID] = copyNode.ID
		nr.Nodes = append(nr.Nodes, &copyNode)
		nr.ByKey[copyNode.Key] = append(nr.ByKey[copyNode.Key], copyNode.ID)
		if copyNode.Parent != nil {
			nr.Nodes[*copyNode.Parent].Children = append(nr.Nodes[*copyNode.Parent].Children, copyNode.ID)
		} else {
			nr.Roots = append(nr.Roots, copyNode.ID)
			nr.Headers = append(nr.Headers, r.Headers[n.Chapter])
		}
	}
	// Convert original sparse chapter indices to compact indices used by dashboard.
	newchapter := map[int]int{}
	for i, root := range nr.Roots {
		newchapter[nr.Nodes[root].Chapter] = i
	}
	for _, n := range nr.Nodes {
		n.Chapter = newchapter[n.Chapter]
	}
	*r = *nr
}
func (r *Repertoire) Lines() {
	for _, n := range r.Nodes {
		idx := n.ID
		parts := []string{}
		for r.Nodes[idx].Parent != nil {
			c := r.Nodes[idx]
			p := r.Nodes[*c.Parent]
			turn := "..."
			if p.Turn == "white" {
				turn = "."
			}
			no := p.board.Full
			parts = append(parts, fmt.Sprintf("%d%s %s", no, turn, c.SAN))
			idx = *c.Parent
		}
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		n.Line = strings.Join(parts, " ")
	}
}
