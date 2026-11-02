// Package games helps with LinkedIn's daily puzzles: it reads a board you
// type in, solves it, and explains the next step the way a person would
// think it through. It never talks to LinkedIn; you play the game.
package games

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Queens is a Queens board: an N×N grid split into N colored regions. A
// solution has one queen in each row, column and region, and no two queens
// touch, not even diagonally.
type Queens struct {
	N      int
	Region [][]rune
	names  []rune // the regions, in the order they first appear
}

// Cell is a square of a board, counted from 0.
type Cell struct{ Row, Col int }

func (c Cell) String() string { return fmt.Sprintf("row %d, column %d", c.Row+1, c.Col+1) }

// MarshalJSON writes a cell as people count, from 1.
func (c Cell) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"row":%d,"column":%d}`, c.Row+1, c.Col+1)), nil
}

// ParseQueens reads a board written one row per line, one letter (or other
// character) per cell naming its region. Spaces are ignored.
func ParseQueens(text string) (*Queens, error) {
	var rows [][]rune
	for _, line := range strings.Split(text, "\n") {
		row := []rune(strings.Join(strings.Fields(line), ""))
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	n := len(rows)
	if n < 4 || n > 12 {
		return nil, fmt.Errorf("a Queens board has 4 to 12 rows; this one has %d", n)
	}
	q := &Queens{N: n, Region: rows}
	seen := map[rune]bool{}
	for r, row := range rows {
		if len(row) != n {
			return nil, fmt.Errorf("row %d has %d cells, but the board has %d rows; it must be square", r+1, len(row), n)
		}
		for _, name := range row {
			if !seen[name] {
				seen[name] = true
				q.names = append(q.names, name)
			}
		}
	}
	if len(q.names) != n {
		return nil, fmt.Errorf("a %d×%d board needs %d regions, one letter each; this one has %d", n, n, n, len(q.names))
	}
	return q, nil
}

// Solve returns the queens of the solution, one per row.
func (q *Queens) Solve() ([]Cell, error) {
	queens := make([]Cell, 0, q.N)
	usedCol := make([]bool, q.N)
	usedRegion := map[rune]bool{}
	var place func(row int) bool
	place = func(row int) bool {
		if row == q.N {
			return true
		}
		for col := 0; col < q.N; col++ {
			region := q.Region[row][col]
			if usedCol[col] || usedRegion[region] {
				continue
			}
			if row > 0 && abs(queens[row-1].Col-col) <= 1 {
				continue
			}
			usedCol[col], usedRegion[region] = true, true
			queens = append(queens, Cell{row, col})
			if place(row + 1) {
				return true
			}
			queens = queens[:row]
			usedCol[col], usedRegion[region] = false, false
		}
		return false
	}
	if !place(0) {
		return nil, errors.New("this board has no solution; check that each letter matches the colors")
	}
	return queens, nil
}

// Step is one deduction: queens to place or cells to cross out, and why.
type Step struct {
	Place  []Cell `json:"place,omitempty"`
	Cross  []Cell `json:"cross,omitempty"`
	Reason string `json:"reason"`
}

// queensState is a board being solved by hand: each cell is free, crossed
// out or holds a queen.
type queensState struct {
	q     *Queens
	cells [][]byte // ' ' free, 'x' crossed out, 'Q' queen
}

func (q *Queens) start(queens []Cell) (*queensState, error) {
	s := &queensState{q: q, cells: make([][]byte, q.N)}
	for r := range s.cells {
		s.cells[r] = []byte(strings.Repeat(" ", q.N))
	}
	for _, c := range queens {
		if c.Row < 0 || c.Row >= q.N || c.Col < 0 || c.Col >= q.N {
			return nil, fmt.Errorf("%s is off the board", c)
		}
		if s.cells[c.Row][c.Col] != ' ' {
			return nil, fmt.Errorf("a queen at %s breaks the rules with the others", c)
		}
		s.queen(c)
	}
	return s, nil
}

// queen puts a queen on c and crosses out the cells it rules out: its row,
// column, region and neighbors. It returns those cells.
func (s *queensState) queen(c Cell) []Cell {
	s.cells[c.Row][c.Col] = 'Q'
	var crossed []Cell
	for r := 0; r < s.q.N; r++ {
		for col := 0; col < s.q.N; col++ {
			if s.cells[r][col] != ' ' {
				continue
			}
			if r == c.Row || col == c.Col || s.q.Region[r][col] == s.q.Region[c.Row][c.Col] || (abs(r-c.Row) <= 1 && abs(col-c.Col) <= 1) {
				s.cells[r][col] = 'x'
				crossed = append(crossed, Cell{r, col})
			}
		}
	}
	return crossed
}

// group is a row, a column or a region, with a name for explanations.
type group struct {
	name  string
	cells []Cell
}

func (s *queensState) groups() []group {
	var groups []group
	for r := 0; r < s.q.N; r++ {
		g := group{name: fmt.Sprintf("row %d", r+1)}
		for c := 0; c < s.q.N; c++ {
			g.cells = append(g.cells, Cell{r, c})
		}
		groups = append(groups, g)
	}
	for c := 0; c < s.q.N; c++ {
		g := group{name: fmt.Sprintf("column %d", c+1)}
		for r := 0; r < s.q.N; r++ {
			g.cells = append(g.cells, Cell{r, c})
		}
		groups = append(groups, g)
	}
	for _, name := range s.q.names {
		g := group{name: fmt.Sprintf("region %c", name)}
		for r := 0; r < s.q.N; r++ {
			for c := 0; c < s.q.N; c++ {
				if s.q.Region[r][c] == name {
					g.cells = append(g.cells, Cell{r, c})
				}
			}
		}
		groups = append(groups, g)
	}
	return groups
}

// free returns the free cells of g, or nil if g already has its queen.
func (s *queensState) free(g group) []Cell {
	var free []Cell
	for _, c := range g.cells {
		switch s.cells[c.Row][c.Col] {
		case 'Q':
			return nil
		case ' ':
			free = append(free, c)
		}
	}
	return free
}

func (s *queensState) solved() bool {
	n := 0
	for _, row := range s.cells {
		n += strings.Count(string(row), "Q")
	}
	return n == s.q.N
}

// next finds the simplest deduction a person would make now, applies it and
// returns it; ok is false when none of the simple ones applies.
func (s *queensState) next() (Step, bool) {
	groups := s.groups()
	// 1. A row, column or region with one free cell left gets its queen.
	for _, g := range groups {
		if free := s.free(g); len(free) == 1 {
			crossed := s.queen(free[0])
			return Step{Place: free, Cross: crossed, Reason: fmt.Sprintf("%s has only one free cell left, so its queen goes at %s", capitalize(g.name), free[0])}, true
		}
	}
	// 2. k regions that fit in k rows (or columns) fill them: cross out the
	// other regions' cells there. And the other way round.
	if step, ok := s.confined(); ok {
		return step, true
	}
	// 3. A queen on a cell that would leave some row, column or region with
	// no free cell can't be there.
	for r := 0; r < s.q.N; r++ {
		for c := 0; c < s.q.N; c++ {
			if s.cells[r][c] != ' ' {
				continue
			}
			if blocked, ok := s.blocks(Cell{r, c}, groups); ok {
				s.cells[r][c] = 'x'
				return Step{Cross: []Cell{{r, c}}, Reason: fmt.Sprintf("A queen at %s would leave %s with no free cell, so it can't go there", Cell{r, c}, blocked)}, true
			}
		}
	}
	// 4. Follow a queen on a cell through the moves it forces; if they end
	// with a row, column or region left empty, it can't go there.
	for r := 0; r < s.q.N; r++ {
		for c := 0; c < s.q.N; c++ {
			if s.cells[r][c] != ' ' {
				continue
			}
			if chain, empty, ok := s.contradiction(Cell{r, c}); ok {
				s.cells[r][c] = 'x'
				return Step{Cross: []Cell{{r, c}}, Reason: fmt.Sprintf("A queen at %s would force %s, and then leave %s with no free cell, so it can't go there",
					Cell{r, c}, joinAnd(chain), empty)}, true
			}
		}
	}
	return Step{}, false
}

// contradiction puts a queen on c in a copy of the board and follows the
// queens that forces, one lone cell after another. It returns those forced
// queens and the group they leave empty, if they do.
func (s *queensState) contradiction(c Cell) ([]string, string, bool) {
	t := s.clone()
	t.queen(c)
	var chain []string
	for range s.q.N {
		if empty := t.empty(); empty != "" {
			return chain, empty, len(chain) > 0
		}
		forced := false
		for _, g := range t.groups() {
			if free := t.free(g); len(free) == 1 {
				t.queen(free[0])
				chain = append(chain, fmt.Sprintf("%s's queen to %s", g.name, free[0]))
				forced = true
				break
			}
		}
		if !forced {
			return nil, "", false
		}
	}
	if empty := t.empty(); empty != "" {
		return chain, empty, len(chain) > 0
	}
	return nil, "", false
}

// empty names a row, column or region with no queen and no free cell.
func (s *queensState) empty() string {
	for _, g := range s.groups() {
		hasQueen, free := false, 0
		for _, c := range g.cells {
			switch s.cells[c.Row][c.Col] {
			case 'Q':
				hasQueen = true
			case ' ':
				free++
			}
		}
		if !hasQueen && free == 0 {
			return g.name
		}
	}
	return ""
}

func (s *queensState) clone() *queensState {
	t := &queensState{q: s.q, cells: make([][]byte, len(s.cells))}
	for i, row := range s.cells {
		t.cells[i] = append([]byte(nil), row...)
	}
	return t
}

// blocks reports which group a queen on c would leave without free cells.
func (s *queensState) blocks(c Cell, groups []group) (string, bool) {
	region := s.q.Region[c.Row][c.Col]
	for _, g := range groups {
		free := s.free(g)
		if free == nil {
			continue
		}
		left := 0
		for _, f := range free {
			same := f.Row == c.Row || f.Col == c.Col || s.q.Region[f.Row][f.Col] == region || (abs(f.Row-c.Row) <= 1 && abs(f.Col-c.Col) <= 1)
			if !same {
				left++
			}
		}
		// The group c itself belongs to is fine: c would be its queen.
		if left == 0 && !contains(free, c) {
			return g.name, true
		}
	}
	return "", false
}

// confined looks for k regions whose free cells all lie in the same k rows
// (or columns): those rows' queens belong to these regions. Likewise k rows
// or columns whose free cells lie in k regions.
func (s *queensState) confined() (Step, bool) {
	groups := s.groups()
	n := s.q.N
	rows, cols, regions := groups[:n], groups[n:2*n], groups[2*n:]
	for _, try := range []struct {
		inner, outer []group
		line         func(Cell) int
		lineName     string
	}{
		{regions, rows, func(c Cell) int { return c.Row }, "row"},
		{regions, cols, func(c Cell) int { return c.Col }, "column"},
	} {
		for k := 1; k < n && k <= 3; k++ {
			for _, combo := range combinations(len(try.inner), k) {
				lines := map[int]bool{}
				names := []string{}
				ok := true
				for _, i := range combo {
					free := s.free(try.inner[i])
					if free == nil {
						ok = false
						break
					}
					names = append(names, try.inner[i].name)
					for _, f := range free {
						lines[try.line(f)] = true
					}
				}
				if !ok || len(lines) != k {
					continue
				}
				var crossed []Cell
				inCombo := map[string]bool{}
				for _, name := range names {
					inCombo[name] = true
				}
				for line := range lines {
					for _, c := range try.outer[line].cells {
						if s.cells[c.Row][c.Col] == ' ' && !inCombo["region "+string(s.q.Region[c.Row][c.Col])] {
							crossed = append(crossed, c)
						}
					}
				}
				if len(crossed) == 0 {
					continue
				}
				for _, c := range crossed {
					s.cells[c.Row][c.Col] = 'x'
				}
				where := plural(k, try.lineName) + " " + joinAnd(lineNames(lines))
				reason := fmt.Sprintf("%s fits only in %s, so that %s's queen is its own: no other region can have a queen there", capitalize(names[0]), where, try.lineName)
				if k > 1 {
					reason = fmt.Sprintf("%s fit only in %s, so those %ss' queens are theirs: no other region can have a queen there", capitalize(joinAnd(names)), where, try.lineName)
				}
				return Step{Cross: crossed, Reason: reason}, true
			}
		}
		// And k rows or columns whose free cells lie in k regions.
		for k := 1; k < n && k <= 3; k++ {
			for _, combo := range combinations(n, k) {
				regionsSeen := map[rune]bool{}
				names := []string{}
				ok := true
				for _, i := range combo {
					free := s.free(try.outer[i])
					if free == nil {
						ok = false
						break
					}
					names = append(names, try.outer[i].name)
					for _, f := range free {
						regionsSeen[s.q.Region[f.Row][f.Col]] = true
					}
				}
				if !ok || len(regionsSeen) != k {
					continue
				}
				inCombo := map[int]bool{}
				for _, i := range combo {
					inCombo[i] = true
				}
				var crossed []Cell
				for r := 0; r < n; r++ {
					for c := 0; c < n; c++ {
						if s.cells[r][c] == ' ' && regionsSeen[s.q.Region[r][c]] && !inCombo[try.line(Cell{r, c})] {
							crossed = append(crossed, Cell{r, c})
						}
					}
				}
				if len(crossed) == 0 {
					continue
				}
				for _, c := range crossed {
					s.cells[c.Row][c.Col] = 'x'
				}
				var regionNames []string
				for name := range regionsSeen {
					regionNames = append(regionNames, "region "+string(name))
				}
				sort.Strings(regionNames)
				reason := fmt.Sprintf("%s can only take its queen from %s, so that region has no queen anywhere else", capitalize(names[0]), regionNames[0])
				if k > 1 {
					reason = fmt.Sprintf("%s can only take their queens from %s, so those regions have no queen anywhere else", capitalize(joinAnd(names)), joinAnd(regionNames))
				}
				return Step{Cross: crossed, Reason: reason}, true
			}
		}
	}
	return Step{}, false
}

// Hint returns the next step from a board with these queens already on it.
func (q *Queens) Hint(queens []Cell) (Step, error) {
	s, err := q.start(queens)
	if err != nil {
		return Step{}, err
	}
	if s.solved() {
		return Step{Reason: "The board is solved."}, nil
	}
	// Crossing out the cells the queens rule out goes without saying; a hint
	// is the first step after that which places a queen or needs a reason.
	if step, ok := s.next(); ok {
		return step, nil
	}
	return Step{}, errors.New("no simple step left: the next move needs trying a queen and seeing where it leads; ask for the solution instead")
}

// Steps solves the board the way a person would and returns every step. If
// the simple deductions run out, the rest comes from the solution.
func (q *Queens) Steps() ([]Step, error) {
	s, _ := q.start(nil)
	var steps []Step
	for !s.solved() {
		step, ok := s.next()
		if !ok {
			solution, err := q.Solve()
			if err != nil {
				return steps, err
			}
			var rest []Cell
			for _, c := range solution {
				if s.cells[c.Row][c.Col] != 'Q' {
					rest = append(rest, c)
				}
			}
			steps = append(steps, Step{Place: rest, Reason: "No simple step is left here; trying queens shows the rest must go like this"})
			break
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// Render draws the board with queens: Q, and the region letters elsewhere.
func (q *Queens) Render(queens []Cell) string {
	at := map[Cell]bool{}
	for _, c := range queens {
		at[c] = true
	}
	var lines []string
	for r := 0; r < q.N; r++ {
		cells := make([]string, q.N)
		for c := 0; c < q.N; c++ {
			cells[c] = strings.ToLower(string(q.Region[r][c]))
			if at[Cell{r, c}] {
				cells[c] = "Q"
			}
		}
		lines = append(lines, strings.Join(cells, " "))
	}
	return strings.Join(lines, "\n")
}

// ParseCells reads cells written "ROW,COLUMN", counted from 1, separated by
// spaces or semicolons, as in "2,5 4,1".
func ParseCells(text string) ([]Cell, error) {
	var cells []Cell
	for _, item := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ';' }) {
		var c Cell
		if _, err := fmt.Sscanf(item, "%d,%d", &c.Row, &c.Col); err != nil {
			return nil, fmt.Errorf("write cells as ROW,COLUMN (from 1), as in 2,5; got %q", item)
		}
		c.Row--
		c.Col--
		cells = append(cells, c)
	}
	return cells, nil
}

func combinations(n, k int) [][]int {
	var out [][]int
	var pick func(start int, chosen []int)
	pick = func(start int, chosen []int) {
		if len(chosen) == k {
			out = append(out, append([]int(nil), chosen...))
			return
		}
		for i := start; i < n; i++ {
			pick(i+1, append(chosen, i))
		}
	}
	pick(0, nil)
	return out
}

func lineNames(lines map[int]bool) []string {
	var nums []int
	for line := range lines {
		nums = append(nums, line+1)
	}
	sort.Ints(nums)
	names := make([]string, len(nums))
	for i, n := range nums {
		names[i] = fmt.Sprint(n)
	}
	return names
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func contains(cells []Cell, c Cell) bool {
	for _, x := range cells {
		if x == c {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
