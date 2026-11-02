package games

import (
	"errors"
	"fmt"
	"strings"
)

// Sudoku is a Mini Sudoku (6×6, boxes of 2 rows by 3 columns) or a classic
// 9×9 one. Zero is an empty cell.
type Sudoku struct {
	N, BoxRows, BoxCols int
	Grid                [][]int
}

// ParseSudoku reads a grid one row per line, a digit per cell and "." or
// "0" for empty ones. Spaces and the "|" or "-" of drawn boxes are ignored.
func ParseSudoku(text string) (*Sudoku, error) {
	var grid [][]int
	for _, line := range strings.Split(text, "\n") {
		var row []int
		for _, r := range line {
			switch {
			case r >= '1' && r <= '9':
				row = append(row, int(r-'0'))
			case r == '.' || r == '0' || r == '_':
				row = append(row, 0)
			case r == ' ' || r == '|' || r == '-' || r == '+' || r == '\t' || r == '\r':
			default:
				return nil, fmt.Errorf("%q is not a digit or a dot", r)
			}
		}
		if len(row) > 0 {
			grid = append(grid, row)
		}
	}
	s := &Sudoku{N: len(grid), Grid: grid}
	switch s.N {
	case 6:
		s.BoxRows, s.BoxCols = 2, 3
	case 9:
		s.BoxRows, s.BoxCols = 3, 3
	default:
		return nil, fmt.Errorf("a Mini Sudoku has 6 rows (or 9 for a classic one); this one has %d", s.N)
	}
	for r, row := range grid {
		if len(row) != s.N {
			return nil, fmt.Errorf("row %d has %d cells; it needs %d", r+1, len(row), s.N)
		}
		for c, v := range row {
			if v > s.N {
				return nil, fmt.Errorf("%d does not fit a %d×%d grid (row %d, column %d)", v, s.N, s.N, r+1, c+1)
			}
			if v != 0 && !s.fits(r, c, v) {
				return nil, fmt.Errorf("the %d at row %d, column %d repeats in its row, column or box", v, r+1, c+1)
			}
		}
	}
	return s, nil
}

// fits reports whether v can go at r, c given the other cells.
func (s *Sudoku) fits(r, c, v int) bool {
	for i := 0; i < s.N; i++ {
		if (i != c && s.Grid[r][i] == v) || (i != r && s.Grid[i][c] == v) {
			return false
		}
	}
	br, bc := r/s.BoxRows*s.BoxRows, c/s.BoxCols*s.BoxCols
	for i := br; i < br+s.BoxRows; i++ {
		for j := bc; j < bc+s.BoxCols; j++ {
			if (i != r || j != c) && s.Grid[i][j] == v {
				return false
			}
		}
	}
	return true
}

func (s *Sudoku) candidates(r, c int) []int {
	var out []int
	for v := 1; v <= s.N; v++ {
		if s.fits(r, c, v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *Sudoku) copyGrid() [][]int {
	grid := make([][]int, s.N)
	for i, row := range s.Grid {
		grid[i] = append([]int(nil), row...)
	}
	return grid
}

// Solve fills the grid, leaving s as it was.
func (s *Sudoku) Solve() ([][]int, error) {
	t := &Sudoku{N: s.N, BoxRows: s.BoxRows, BoxCols: s.BoxCols, Grid: s.copyGrid()}
	var fill func() bool
	fill = func() bool {
		// The emptiest cell first, as a person would look.
		best, bestR, bestC := []int(nil), -1, -1
		for r := 0; r < t.N; r++ {
			for c := 0; c < t.N; c++ {
				if t.Grid[r][c] == 0 {
					if cands := t.candidates(r, c); bestR < 0 || len(cands) < len(best) {
						best, bestR, bestC = cands, r, c
					}
				}
			}
		}
		if bestR < 0 {
			return true
		}
		for _, v := range best {
			t.Grid[bestR][bestC] = v
			if fill() {
				return true
			}
		}
		t.Grid[bestR][bestC] = 0
		return false
	}
	if !fill() {
		return nil, errors.New("this grid has no solution; check the digits")
	}
	return t.Grid, nil
}

// SudokuStep is one digit placed, and why.
type SudokuStep struct {
	Cell   Cell   `json:"cell"`
	Digit  int    `json:"digit"`
	Reason string `json:"reason"`
}

// next finds the simplest deduction: a cell with one possible digit, or a
// digit with one possible cell in a row, column or box.
func (s *Sudoku) next() (SudokuStep, bool) {
	for r := 0; r < s.N; r++ {
		for c := 0; c < s.N; c++ {
			if s.Grid[r][c] != 0 {
				continue
			}
			if cands := s.candidates(r, c); len(cands) == 1 {
				return SudokuStep{Cell{r, c}, cands[0], fmt.Sprintf("Every other digit is already in the row, column or box of %s, so it can only be %d", Cell{r, c}, cands[0])}, true
			}
		}
	}
	for _, g := range s.houses() {
		for v := 1; v <= s.N; v++ {
			var spots []Cell
			present := false
			for _, cell := range g.cells {
				if s.Grid[cell.Row][cell.Col] == v {
					present = true
					break
				}
				if s.Grid[cell.Row][cell.Col] == 0 && s.fits(cell.Row, cell.Col, v) {
					spots = append(spots, cell)
				}
			}
			if !present && len(spots) == 1 {
				return SudokuStep{spots[0], v, fmt.Sprintf("%s needs a %d, and %s is the only cell where it fits", capitalize(g.name), v, spots[0])}, true
			}
		}
	}
	return SudokuStep{}, false
}

func (s *Sudoku) houses() []group {
	var houses []group
	for r := 0; r < s.N; r++ {
		g := group{name: fmt.Sprintf("row %d", r+1)}
		for c := 0; c < s.N; c++ {
			g.cells = append(g.cells, Cell{r, c})
		}
		houses = append(houses, g)
	}
	for c := 0; c < s.N; c++ {
		g := group{name: fmt.Sprintf("column %d", c+1)}
		for r := 0; r < s.N; r++ {
			g.cells = append(g.cells, Cell{r, c})
		}
		houses = append(houses, g)
	}
	box := 0
	for br := 0; br < s.N; br += s.BoxRows {
		for bc := 0; bc < s.N; bc += s.BoxCols {
			box++
			g := group{name: fmt.Sprintf("box %d", box)}
			for r := br; r < br+s.BoxRows; r++ {
				for c := bc; c < bc+s.BoxCols; c++ {
					g.cells = append(g.cells, Cell{r, c})
				}
			}
			houses = append(houses, g)
		}
	}
	return houses
}

// Hint returns the next digit a person could place, and why.
func (s *Sudoku) Hint() (SudokuStep, error) {
	if s.full() {
		return SudokuStep{Reason: "The grid is solved."}, nil
	}
	if step, ok := s.next(); ok {
		return step, nil
	}
	return SudokuStep{}, errors.New("no simple step left: the next digit needs trying one and seeing where it leads; ask for the solution instead")
}

// Steps fills the grid the way a person would and returns every digit, in
// order; if the simple steps run out, the rest comes from the solution.
func (s *Sudoku) Steps() ([]SudokuStep, error) {
	t := &Sudoku{N: s.N, BoxRows: s.BoxRows, BoxCols: s.BoxCols, Grid: s.copyGrid()}
	var steps []SudokuStep
	for !t.full() {
		step, ok := t.next()
		if !ok {
			solution, err := s.Solve()
			if err != nil {
				return steps, err
			}
			for r := range solution {
				for c := range solution[r] {
					if t.Grid[r][c] == 0 {
						steps = append(steps, SudokuStep{Cell{r, c}, solution[r][c], "No simple step is left here; trying digits shows it must be this"})
					}
				}
			}
			break
		}
		t.Grid[step.Cell.Row][step.Cell.Col] = step.Digit
		steps = append(steps, step)
	}
	return steps, nil
}

func (s *Sudoku) full() bool {
	for _, row := range s.Grid {
		for _, v := range row {
			if v == 0 {
				return false
			}
		}
	}
	return true
}

// RenderGrid draws a grid with its boxes.
func (s *Sudoku) RenderGrid(grid [][]int) string {
	var lines []string
	for r, row := range grid {
		if r > 0 && r%s.BoxRows == 0 {
			parts := make([]string, s.N/s.BoxCols)
			for i := range parts {
				parts[i] = strings.Repeat("-", s.BoxCols*2-1)
			}
			lines = append(lines, strings.Join(parts, "-+-"))
		}
		var b strings.Builder
		for c, v := range row {
			if c > 0 {
				if c%s.BoxCols == 0 {
					b.WriteString(" | ")
				} else {
					b.WriteString(" ")
				}
			}
			if v == 0 {
				b.WriteString(".")
			} else {
				fmt.Fprint(&b, v)
			}
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}
