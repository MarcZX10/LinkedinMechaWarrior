package games

import (
	"strings"
	"testing"
)

const mini = `
1 . . | . 5 6
. 5 6 | 1 . .
------+------
2 . . | . 6 4
. 6 4 | 2 . .
------+------
3 . . | . 4 5
. 4 5 | 3 . .
`

func solvedOK(t *testing.T, s *Sudoku, grid [][]int) {
	t.Helper()
	check := &Sudoku{N: s.N, BoxRows: s.BoxRows, BoxCols: s.BoxCols, Grid: grid}
	for r := range grid {
		for c, v := range grid[r] {
			if v < 1 || v > s.N || !check.fits(r, c, v) {
				t.Fatalf("bad digit %d at %s:\n%s", v, Cell{r, c}, s.RenderGrid(grid))
			}
			if given := s.Grid[r][c]; given != 0 && given != v {
				t.Fatalf("changed a given digit at %s", Cell{r, c})
			}
		}
	}
}

func TestMiniSudoku(t *testing.T) {
	s, err := ParseSudoku(mini)
	if err != nil {
		t.Fatal(err)
	}
	if s.BoxRows != 2 || s.BoxCols != 3 {
		t.Fatalf("boxes %dx%d", s.BoxRows, s.BoxCols)
	}
	solution, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	solvedOK(t, s, solution)
	if s.Grid[0][1] != 0 {
		t.Error("Solve changed the grid it was given")
	}

	steps, err := s.Steps()
	if err != nil {
		t.Fatal(err)
	}
	grid := s.copyGrid()
	for _, step := range steps {
		if step.Reason == "" {
			t.Errorf("step without a reason: %+v", step)
		}
		grid[step.Cell.Row][step.Cell.Col] = step.Digit
	}
	solvedOK(t, s, grid)

	hint, err := s.Hint()
	if err != nil || hint.Digit == 0 || !strings.Contains(hint.Reason, hint.Cell.String()) {
		t.Errorf("hint %+v, %v", hint, err)
	}
	if rendered := s.RenderGrid(solution); !strings.Contains(rendered, " | ") || !strings.Contains(rendered, "-+-") {
		t.Errorf("render\n%s", rendered)
	}
}

func TestNakedAndHiddenSingles(t *testing.T) {
	// Row 1 has 1 to 4; column 1 has a 6 lower down, so row 1, column 1
	// can only be 5.
	s, err := ParseSudoku(`
. 1 . | 2 3 4
. . . | . 6 .
6 . . | . . .
. . . | . . .
. . . | . . .
. . . | . . .
`)
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.Hint()
	if err != nil || step.Cell != (Cell{0, 0}) || step.Digit != 5 || !strings.Contains(step.Reason, "can only be 5") {
		t.Errorf("naked single %+v, %v", step, err)
	}

	// Row 1 needs a 1: box 2 has one, and so do columns 1 and 2, so only
	// column 3 can take it.
	s, err = ParseSudoku(`
. . . | . . .
. . . | . 1 .
. . . | . . .
1 . . | . . .
. 1 . | . . .
. . . | . . .
`)
	if err != nil {
		t.Fatal(err)
	}
	step, err = s.Hint()
	if err != nil || step.Cell != (Cell{0, 2}) || step.Digit != 1 || step.Reason != "Row 1 needs a 1, and row 1, column 3 is the only cell where it fits" {
		t.Errorf("hidden single %+v, %v", step, err)
	}
	if solution, err := s.Solve(); err != nil {
		t.Fatal(err)
	} else {
		solvedOK(t, s, solution)
	}
}

func TestParseSudokuErrors(t *testing.T) {
	for _, bad := range []string{"", "12\n21", "1 1 . . . .\n......\n......\n......\n......\n......", "7.....\n......\n......\n......\n......\n......", "x....."} {
		if _, err := ParseSudoku(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
