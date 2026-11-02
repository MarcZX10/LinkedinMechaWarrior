package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const queensBoard = "AABBB\nAABBB\nCCZBB\nCCDDD\nCCCCD\n"

func TestGamesQueensSolvesHintsAndExplains(t *testing.T) {
	h := newHarness(t)
	out := h.mustRun(queensBoard+"\n", "games", "queens")
	if strings.Count(out, "Q") != 5 {
		t.Errorf("solution\n%s", out)
	}
	file := filepath.Join(t.TempDir(), "board.txt")
	if err := os.WriteFile(file, []byte(queensBoard), 0o600); err != nil {
		t.Fatal(err)
	}
	out = h.mustRun("", "games", "queens", file, "--hint")
	if !strings.Contains(out, "Region Z has only one free cell left, so its queen goes at row 3, column 3.") {
		t.Errorf("hint %s", out)
	}
	out = h.mustRun("", "games", "queens", file, "--hint", "--queens", "3,3")
	if strings.Contains(out, "Region Z") {
		t.Errorf("hint ignored the placed queen: %s", out)
	}
	out = h.mustRun("", "games", "queens", file, "--steps")
	explained, board, _ := strings.Cut(out, "\n\n")
	if !strings.HasPrefix(explained, "1. Region Z") || strings.Count(board, "Q") != 5 {
		t.Errorf("steps\n%s", out)
	}
	var result struct {
		Solution []struct{ Row, Column int } `json:"solution"`
	}
	if err := json.Unmarshal([]byte(h.mustRun("", "--json", "games", "queens", file)), &result); err != nil || len(result.Solution) != 5 || result.Solution[0].Row != 1 {
		t.Errorf("json %+v, %v", result, err)
	}
	if len(h.linkedin.requests) != 0 {
		t.Errorf("games sent %d requests", len(h.linkedin.requests))
	}
	if code, _, errOut := h.run("AB\nBA\n\n", "games", "queens"); code == 0 || !strings.Contains(errOut, "4 to 12 rows") {
		t.Errorf("bad board: exit %d, %s", code, errOut)
	}
}

func TestGamesSudoku(t *testing.T) {
	h := newHarness(t)
	grid := ". 1 . | 2 3 4\n. . . | . 6 .\n6 . . | . . .\n. . . | . . .\n. . . | . . .\n. . . | . . .\n\n"
	if out := h.mustRun(grid, "games", "sudoku", "--hint"); !strings.Contains(out, "Write 5 at row 1, column 1.") {
		t.Errorf("hint %s", out)
	}
	if out := h.mustRun(grid, "games", "sudoku"); strings.Contains(out, ".") || !strings.Contains(out, " | ") {
		t.Errorf("solution\n%s", out)
	}
}

func TestAssistantsGetPuzzleHints(t *testing.T) {
	h := newHarness(t)
	board, _ := json.Marshal(queensBoard)
	answers := h.mcpSession(nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"solve_queens","arguments":{"board":`+string(board)+`}}}`,
	)
	if text, isErr := mcpText(t, answers[2]); isErr || !strings.Contains(text, "Region Z has only one free cell left") || !strings.Contains(text, `"column": 3`) {
		t.Errorf("solve_queens %s", text)
	}
}
