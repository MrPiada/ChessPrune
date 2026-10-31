package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// 0x88 board coordinates: a8=0, h1=119. This engine validates SAN in standard chess PGNs.
type Board struct {
	P          [128]byte
	White      bool
	Castling   string
	EP         int
	Half, Full int
}
type Move struct {
	From, To          int
	Promo             byte
	Castle, EnPassant bool
}

const initialFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

func on(i int) bool   { return i >= 0 && i < 128 && i&0x88 == 0 }
func sq(i int) string { return string(byte('a'+i%16)) + strconv.Itoa(8-i/16) }
func ix(s string) int {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return -1
	}
	return int('8'-s[1])*16 + int(s[0]-'a')
}
func white(p byte) bool        { return p >= 'A' && p <= 'Z' }
func same(p byte, w bool) bool { return p != '.' && white(p) == w }
func lower(p byte) byte {
	if p >= 'A' && p <= 'Z' {
		return p + 32
	}
	return p
}
func upper(p byte) byte {
	if p >= 'a' && p <= 'z' {
		return p - 32
	}
	return p
}
func ParseFEN(fen string) (Board, error) {
	var b Board
	for i := range b.P {
		b.P[i] = '.'
	}
	f := strings.Fields(fen)
	if len(f) < 4 {
		return b, fmt.Errorf("invalid FEN: %q", fen)
	}
	rows := strings.Split(f[0], "/")
	if len(rows) != 8 {
		return b, fmt.Errorf("invalid FEN ranks")
	}
	for r, row := range rows {
		c := 0
		for _, p := range row {
			if p >= '1' && p <= '8' {
				c += int(p - '0')
			} else {
				if c >= 8 || !strings.ContainsRune("pnbrqkPNBRQK", p) {
					return b, fmt.Errorf("invalid FEN piece")
				}
				b.P[r*16+c] = byte(p)
				c++
			}
		}
		if c != 8 {
			return b, fmt.Errorf("invalid FEN row")
		}
	}
	if f[1] != "w" && f[1] != "b" {
		return b, fmt.Errorf("invalid FEN side")
	}
	b.White = f[1] == "w"
	b.Castling = f[2]
	b.EP = -1
	if f[3] != "-" {
		b.EP = ix(f[3])
		if b.EP < 0 {
			return b, fmt.Errorf("invalid FEN en passant")
		}
	}
	if len(f) > 4 {
		b.Half, _ = strconv.Atoi(f[4])
	}
	b.Full = 1
	if len(f) > 5 {
		b.Full, _ = strconv.Atoi(f[5])
	}
	if b.Full < 1 {
		b.Full = 1
	}
	wk, bk := 0, 0
	for i := 0; i < 128; i++ {
		if !on(i) {
			continue
		}
		if b.P[i] == 'K' {
			wk++
		}
		if b.P[i] == 'k' {
			bk++
		}
	}
	if wk != 1 || bk != 1 {
		return b, fmt.Errorf("FEN must contain one king of each side")
	}
	return b, nil
}
func (b Board) FEN() string {
	parts := []string{}
	for r := 0; r < 8; r++ {
		row := ""
		empty := 0
		for c := 0; c < 8; c++ {
			p := b.P[r*16+c]
			if p == '.' {
				empty++
				continue
			}
			if empty > 0 {
				row += strconv.Itoa(empty)
				empty = 0
			}
			row += string(p)
		}
		if empty > 0 {
			row += strconv.Itoa(empty)
		}
		parts = append(parts, row)
	}
	turn := "b"
	if b.White {
		turn = "w"
	}
	cast := b.Castling
	if cast == "" {
		cast = "-"
	}
	ep := "-"
	if b.EP >= 0 {
		for _, m := range b.moves() {
			if m.EnPassant {
				ep = sq(b.EP)
				break
			}
		}
	}
	return strings.Join(parts, "/") + fmt.Sprintf(" %s %s %s %d %d", turn, cast, ep, b.Half, b.Full)
}
func (b Board) Key() string { return strings.Join(strings.Fields(b.FEN())[:4], " ") }
func (b Board) attacked(target int, byWhite bool) bool {
	knight := []int{-33, -31, -18, -14, 14, 18, 31, 33}
	diagonals := []int{-17, -15, 15, 17}
	straights := []int{-16, -1, 1, 16}
	king := []int{-17, -16, -15, -1, 1, 15, 16, 17}
	for f := 0; f < 128; f++ {
		if !on(f) || !same(b.P[f], byWhite) {
			continue
		}
		p := lower(b.P[f])
		if p == 'p' {
			delta := 16
			if byWhite {
				delta = -16
			}
			if f+delta-1 == target || f+delta+1 == target {
				return true
			}
			continue
		}
		ds := []int{}
		switch p {
		case 'n':
			ds = knight
		case 'b':
			ds = diagonals
		case 'r':
			ds = straights
		case 'q', 'k':
			ds = king
		}
		for _, d := range ds {
			for t := f + d; on(t); t += d {
				if t == target {
					return true
				}
				if b.P[t] != '.' || p == 'n' || p == 'k' {
					break
				}
			}
		}
	}
	return false
}
func (b Board) check(w bool) bool {
	k := byte('k')
	if w {
		k = 'K'
	}
	for i := 0; i < 128; i++ {
		if on(i) && b.P[i] == k {
			return b.attacked(i, !w)
		}
	}
	return true
}
func (b Board) Apply(m Move) Board {
	x := b
	p := x.P[m.From]
	cap := x.P[m.To]
	x.P[m.From] = '.'
	x.P[m.To] = p
	if m.Promo != 0 {
		q := m.Promo
		if b.White {
			q = upper(q)
		}
		x.P[m.To] = q
	}
	if m.EnPassant {
		if b.White {
			x.P[m.To+16] = '.'
		} else {
			x.P[m.To-16] = '.'
		}
	}
	if m.Castle {
		rf, rt := 0, 0
		if m.To > m.From {
			rf = m.From + 3
			rt = m.From + 1
		} else {
			rf = m.From - 4
			rt = m.From - 1
		}
		x.P[rt] = x.P[rf]
		x.P[rf] = '.'
	}
	if p == 'K' {
		x.Castling = strings.ReplaceAll(strings.ReplaceAll(x.Castling, "K", ""), "Q", "")
	}
	if p == 'k' {
		x.Castling = strings.ReplaceAll(strings.ReplaceAll(x.Castling, "k", ""), "q", "")
	}
	rbits := map[int]string{119: "K", 112: "Q", 7: "k", 0: "q"}
	if lower(p) == 'r' {
		x.Castling = strings.ReplaceAll(x.Castling, rbits[m.From], "")
	}
	if lower(cap) == 'r' {
		x.Castling = strings.ReplaceAll(x.Castling, rbits[m.To], "")
	}
	x.EP = -1
	if lower(p) == 'p' && m.To-m.From == 32 {
		x.EP = m.From + 16
	}
	if lower(p) == 'p' && m.From-m.To == 32 {
		x.EP = m.From - 16
	}
	x.Half++
	if lower(p) == 'p' || cap != '.' || m.EnPassant {
		x.Half = 0
	}
	if !b.White {
		x.Full++
	}
	x.White = !b.White
	return x
}
func (b Board) moves() []Move {
	out := []Move{}
	push := func(f, t int, castle, ep bool) {
		if !on(t) || lower(b.P[t]) == 'k' {
			return
		}
		if lower(b.P[f]) == 'p' && (t/16 == 0 || t/16 == 7) {
			for _, p := range []byte{'q', 'r', 'b', 'n'} {
				out = append(out, Move{f, t, p, castle, ep})
			}
		} else {
			out = append(out, Move{f, t, 0, castle, ep})
		}
	}
	knight := []int{-33, -31, -18, -14, 14, 18, 31, 33}
	diag := []int{-17, -15, 15, 17}
	straight := []int{-16, -1, 1, 16}
	king := []int{-17, -16, -15, -1, 1, 15, 16, 17}
	for f := 0; f < 128; f++ {
		if !on(f) || !same(b.P[f], b.White) {
			continue
		}
		p := lower(b.P[f])
		if p == 'p' {
			d := 16
			row := 1
			enemy := byte('P')
			if b.White {
				d = -16
				row = 6
				enemy = 'p'
			}
			t := f + d
			if on(t) && b.P[t] == '.' {
				push(f, t, false, false)
				u := t + d
				if f/16 == row && on(u) && b.P[u] == '.' {
					push(f, u, false, false)
				}
			}
			for _, col := range []int{-1, 1} {
				u := t + col
				if !on(u) {
					continue
				}
				if b.P[u] != '.' && same(b.P[u], !b.White) {
					push(f, u, false, false)
				} else if u == b.EP && b.P[u] == '.' && b.P[u-d] == enemy {
					push(f, u, false, true)
				}
			}
			continue
		}
		ds := []int{}
		switch p {
		case 'n':
			ds = knight
		case 'b':
			ds = diag
		case 'r':
			ds = straight
		case 'q', 'k':
			ds = king
		}
		for _, d := range ds {
			for t := f + d; on(t); t += d {
				if same(b.P[t], b.White) {
					break
				}
				push(f, t, false, false)
				if b.P[t] != '.' || p == 'n' || p == 'k' {
					break
				}
			}
		}
		if p == 'k' && !b.check(b.White) {
			home, rook := 4, byte('k')
			if b.White {
				home = 116
				rook = 'K'
			}
			_ = rook
			if f == home {
				if strings.Contains(b.Castling, func() string {
					if b.White {
						return "K"
					}
					return "k"
				}()) && b.P[f+3] == func() byte {
					if b.White {
						return 'R'
					}
					return 'r'
				}() && b.P[f+1] == '.' && b.P[f+2] == '.' && !b.attacked(f+1, !b.White) && !b.attacked(f+2, !b.White) {
					push(f, f+2, true, false)
				}
				if strings.Contains(b.Castling, func() string {
					if b.White {
						return "Q"
					}
					return "q"
				}()) && b.P[f-4] == func() byte {
					if b.White {
						return 'R'
					}
					return 'r'
				}() && b.P[f-1] == '.' && b.P[f-2] == '.' && b.P[f-3] == '.' && !b.attacked(f-1, !b.White) && !b.attacked(f-2, !b.White) {
					push(f, f-2, true, false)
				}
			}
		}
	}
	legal := []Move{}
	for _, m := range out {
		if !b.Apply(m).check(b.White) {
			legal = append(legal, m)
		}
	}
	return legal
}
func (b Board) SAN(m Move) string {
	if m.Castle {
		if m.To > m.From {
			return "O-O"
		}
		return "O-O-O"
	}
	p := upper(b.P[m.From])
	capture := b.P[m.To] != '.' || m.EnPassant
	v := ""
	if p != 'P' {
		v = string(p)
		others := []Move{}
		for _, o := range b.moves() {
			if o.From != m.From && o.To == m.To && b.P[o.From] == b.P[m.From] {
				others = append(others, o)
			}
		}
		if len(others) > 0 {
			sameFile, sameRank := false, false
			for _, o := range others {
				if o.From%16 == m.From%16 {
					sameFile = true
				}
				if o.From/16 == m.From/16 {
					sameRank = true
				}
			}
			if !sameFile {
				v += sq(m.From)[:1]
			} else if !sameRank {
				v += sq(m.From)[1:]
			} else {
				v += sq(m.From)
			}
		}
	} else if capture {
		v = sq(m.From)[:1]
	}
	if capture {
		v += "x"
	}
	v += sq(m.To)
	if m.Promo != 0 {
		v += "=" + string(upper(m.Promo))
	}
	next := b.Apply(m)
	if next.check(next.White) {
		if len(next.moves()) == 0 {
			v += "#"
		} else {
			v += "+"
		}
	}
	return v
}

var sanRE = regexp.MustCompile(`^([KQRBN])?([a-h])?([1-8])?(x)?([a-h][1-8])(?:=?([QRBN]))?$`)

func (b Board) ParseSAN(s string) (Move, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "!?+#")
	s = strings.ReplaceAll(s, "0", "O")
	s = strings.TrimSuffix(s, "e.p.")
	s = strings.TrimSpace(s)
	if s == "O-O" || s == "O-O-O" {
		for _, m := range b.moves() {
			if m.Castle && ((s == "O-O" && m.To > m.From) || (s == "O-O-O" && m.To < m.From)) {
				return m, nil
			}
		}
		return Move{}, fmt.Errorf("illegal castling %q", s)
	}
	x := sanRE.FindStringSubmatch(s)
	if x == nil {
		return Move{}, fmt.Errorf("invalid SAN: %q", s)
	}
	piece := byte('P')
	if x[1] != "" {
		piece = byte(unicode.ToUpper(rune(x[1][0])))
	}
	t := ix(x[5])
	poss := []Move{}
	for _, m := range b.moves() {
		if m.To != t || upper(b.P[m.From]) != piece {
			continue
		}
		if x[2] != "" && sq(m.From)[0] != x[2][0] {
			continue
		}
		if x[3] != "" && sq(m.From)[1] != x[3][0] {
			continue
		}
		if (b.P[m.To] != '.' || m.EnPassant) != (x[4] != "") {
			continue
		}
		if x[6] != "" {
			if upper(m.Promo) != byte(unicode.ToUpper(rune(x[6][0]))) {
				continue
			}
		} else if m.Promo != 0 {
			continue
		}
		poss = append(poss, m)
	}
	if len(poss) != 1 {
		return Move{}, fmt.Errorf("SAN %q: %d legal matches at %s", s, len(poss), b.FEN())
	}
	return poss[0], nil
}
