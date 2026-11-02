package games

import (
	"strings"
	"testing"
)

const board6 = `
A A A B B C
A A B B C C
D D B B C C
D D E E F C
D E E E F F
D E E F F F
`

// valid checks the rules of Queens.
func valid(t *testing.T, q *Queens, queens []Cell) {
	t.Helper()
	if len(queens) != q.N {
		t.Fatalf("%d queens on a %d board", len(queens), q.N)
	}
	rows, cols, regions := map[int]bool{}, map[int]bool{}, map[rune]bool{}
	for i, a := range queens {
		if rows[a.Row] || cols[a.Col] || regions[q.Region[a.Row][a.Col]] {
			t.Fatalf("two queens share a row, column or region: %v", queens)
		}
		rows[a.Row], cols[a.Col], regions[q.Region[a.Row][a.Col]] = true, true, true
		for _, b := range queens[i+1:] {
			if abs(a.Row-b.Row) <= 1 && abs(a.Col-b.Col) <= 1 {
				t.Fatalf("queens at %s and %s touch", a, b)
			}
		}
	}
}

func TestSolveQueens(t *testing.T) {
	q, err := ParseQueens(board6)
	if err != nil {
		t.Fatal(err)
	}
	solution, err := q.Solve()
	if err != nil {
		t.Fatal(err)
	}
	valid(t, q, solution)
	if rendered := q.Render(solution); strings.Count(rendered, "Q") != 6 || len(strings.Split(rendered, "\n")) != 6 {
		t.Errorf("render\n%s", rendered)
	}
}

func TestStepsReachASolutionWithReasons(t *testing.T) {
	q, _ := ParseQueens(board6)
	steps, err := q.Steps()
	if err != nil {
		t.Fatal(err)
	}
	var queens []Cell
	for _, s := range steps {
		if s.Reason == "" {
			t.Errorf("step without a reason: %+v", s)
		}
		queens = append(queens, s.Place...)
	}
	valid(t, q, queens)
}

func TestHintFindsTheLoneCell(t *testing.T) {
	// Region Z is a single cell: its queen can only go there.
	q, err := ParseQueens("AAAA\nBBBZ\nCCCC\nDDDD")
	if err == nil {
		t.Fatal("a 4×4 board with 5 regions was accepted")
	}
	q, err = ParseQueens(`
AABBB
AABBB
CCZBB
CCDDD
CCCCD
`)
	if err != nil {
		t.Fatal(err)
	}
	step, err := q.Hint(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.Place) != 1 || step.Place[0] != (Cell{2, 2}) || !strings.Contains(step.Reason, "Region Z has only one free cell left") {
		t.Errorf("hint %+v", step)
	}
	// With that queen placed, the next hint is another step.
	next, err := q.Hint([]Cell{{2, 2}})
	if err != nil || next.Reason == "" || (len(next.Place) == 1 && next.Place[0] == (Cell{2, 2})) {
		t.Errorf("next hint %+v, %v", next, err)
	}
	steps, err := q.Steps()
	if err != nil {
		t.Fatal(err)
	}
	var queens []Cell
	for _, step := range steps {
		if strings.HasPrefix(step.Reason, "No simple step") {
			t.Errorf("needed the solution: %+v", steps)
		}
		queens = append(queens, step.Place...)
	}
	valid(t, q, queens)
	if _, err := q.Hint([]Cell{{2, 2}, {2, 3}}); err == nil {
		t.Error("two queens side by side were accepted")
	}
}

func TestPigeonholeHint(t *testing.T) {
	// Regions A and B live only in rows 1 and 2: no other region may have a
	// queen in those rows, so the first hint crosses out C's cells there.
	q, err := ParseQueens(`
AABBCC
AABBCC
DDEEFF
DDEEFF
DDEEFF
DDEEFF
`)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := q.start(nil)
	step, ok := s.confined()
	if !ok || !strings.Contains(step.Reason, "fit only in rows 1 and 2") || len(step.Cross) != 4 {
		t.Errorf("step %+v", step)
	}
	for _, c := range step.Cross {
		if q.Region[c.Row][c.Col] != 'C' {
			t.Errorf("crossed %s of region %c", c, q.Region[c.Row][c.Col])
		}
	}
}

func TestParseQueensErrors(t *testing.T) {
	for _, bad := range []string{"", "AB\nBA", "AAB\nABB\nCCC\nDDD", "ABCD\nABCD\nABCD"} {
		if _, err := ParseQueens(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if cells, err := ParseCells("2,5 4,1"); err != nil || len(cells) != 2 || cells[0] != (Cell{1, 4}) {
		t.Errorf("cells %v, %v", cells, err)
	}
	if _, err := ParseCells("b5"); err == nil {
		t.Error("b5 accepted")
	}
}

func TestFollowingAQueenThroughForcedMoves(t *testing.T) {
	// After the lone cell's queen, a queen at row 1, column 1 forces row 2's
	// queen to column 5, which leaves row 4 with no free cell.
	q, err := ParseQueens("AABBB\nAABBB\nCCZBB\nCCDDD\nCCDDD")
	if err != nil {
		t.Fatal(err)
	}
	s, err := q.start([]Cell{{2, 2}})
	if err != nil {
		t.Fatal(err)
	}
	chain, empty, ok := s.contradiction(Cell{0, 0})
	if !ok || empty != "row 4" || len(chain) != 1 || chain[0] != "row 2's queen to row 2, column 5" {
		t.Errorf("chain %q, empty %q, %v", chain, empty, ok)
	}
	if _, _, ok := s.contradiction(Cell{1, 0}); ok {
		t.Error("a queen of a solution was ruled out")
	}
}
