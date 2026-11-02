package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/games"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
)

func (a *app) gamesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "games",
		Short: "Solve LinkedIn's daily puzzles, or get a hint (offline)",
		Long: `Help with LinkedIn's daily puzzles: type in the board, and lmw solves it or
explains the next step the way a person would think it through.

lmw never opens the games on LinkedIn and never plays for you: you play,
and these commands send no request at all.

The board comes from FILE, or from what you type or paste (end it with an
empty line).`,
	}
	var hint, steps bool
	var placed string
	queens := &cobra.Command{
		Use:   "queens [FILE]",
		Short: "Solve a Queens board, or explain the next step",
		Long: `Solve a Queens board, or explain the next step.

Write the board one row per line, one letter per cell for its color: the
same letter for the same region, as in

  A A B B B
  A A B B B
  C C Z B B
  C C D D D
  C C C C D`,
		Example: "  lmw games queens board.txt\n  lmw games queens board.txt --hint\n  lmw games queens board.txt --hint --queens \"3,3 1,4\"\n  lmw games queens board.txt --steps",
		Args:    rangeArgs(0, 1, "at most one file with the board"),
		RunE: func(_ *cobra.Command, args []string) error {
			text, err := a.readBoard(args)
			if err != nil {
				return err
			}
			board, err := games.ParseQueens(text)
			if err != nil {
				return errs.New(errs.Usage, "%s", err)
			}
			result, err := queensResult(board, hint, steps, placed)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, result)
			}
			a.printf("%s\n", result.text)
			return nil
		},
	}
	queens.Flags().BoolVar(&hint, "hint", false, "only explain the next step")
	queens.Flags().BoolVar(&steps, "steps", false, "explain every step to the solution")
	queens.Flags().StringVar(&placed, "queens", "", `with --hint, the queens you already placed, as ROW,COLUMN from 1: "3,3 1,4"`)

	var sudokuHint, sudokuSteps bool
	sudoku := &cobra.Command{
		Use:   "sudoku [FILE]",
		Short: "Solve a Mini Sudoku (or a classic one), or explain the next digit",
		Long: `Solve a Mini Sudoku (6×6, boxes of 2 by 3) or a classic 9×9 sudoku, or
explain the next digit.

Write the grid one row per line, a digit per cell and a dot for empty ones.
Spaces and drawn box lines (| - +) are ignored:

  1 . . | . 5 6
  . 5 6 | 1 . .`,
		Example: "  lmw games sudoku grid.txt\n  lmw games sudoku grid.txt --hint",
		Args:    rangeArgs(0, 1, "at most one file with the grid"),
		RunE: func(_ *cobra.Command, args []string) error {
			text, err := a.readBoard(args)
			if err != nil {
				return err
			}
			grid, err := games.ParseSudoku(text)
			if err != nil {
				return errs.New(errs.Usage, "%s", err)
			}
			result, err := sudokuResult(grid, sudokuHint, sudokuSteps)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, result)
			}
			a.printf("%s\n", result.text)
			return nil
		},
	}
	sudoku.Flags().BoolVar(&sudokuHint, "hint", false, "only explain the next digit")
	sudoku.Flags().BoolVar(&sudokuSteps, "steps", false, "explain every digit to the solution")
	cmd.AddCommand(queens, sudoku)
	return cmd
}

// readBoard reads a board from the file named in args, or from stdin until
// an empty line after the board.
func (a *app) readBoard(args []string) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		raw, err := os.ReadFile(args[0])
		if err != nil {
			return "", errs.New(errs.Usage, "cannot read the board: %v", err)
		}
		return string(raw), nil
	}
	if a.stdinFD >= 0 && term.IsTerminal(a.stdinFD) {
		a.note("Type or paste the board, one row per line, then an empty line:\n")
	}
	var lines []string
	scanner := bufio.NewScanner(a.stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" && len(lines) > 0 {
			break
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "", errs.New(errs.Usage, "no board: give a file, or type the board and end it with an empty line")
	}
	return strings.Join(lines, "\n"), nil
}

// gameResult is what a games command or tool answers: data, and the same
// for people.
type gameResult struct {
	Solution any    `json:"solution,omitempty"`
	Board    string `json:"board,omitempty"`
	Hint     any    `json:"hint,omitempty"`
	Steps    any    `json:"steps,omitempty"`
	text     string
}

func queensResult(board *games.Queens, hint, steps bool, placed string) (gameResult, error) {
	switch {
	case hint:
		queens, err := games.ParseCells(placed)
		if err != nil {
			return gameResult{}, errs.New(errs.Usage, "%s", err)
		}
		step, err := board.Hint(queens)
		if err != nil {
			return gameResult{}, errs.New(errs.Usage, "%s", err)
		}
		return gameResult{Hint: step, text: describeQueensStep(step)}, nil
	case steps:
		all, err := board.Steps()
		if err != nil {
			return gameResult{}, errs.New(errs.Usage, "%s", err)
		}
		var lines []string
		var queens []games.Cell
		for i, step := range all {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, describeQueensStep(step)))
			queens = append(queens, step.Place...)
		}
		rendered := board.Render(queens)
		return gameResult{Steps: all, Board: rendered, text: strings.Join(lines, "\n") + "\n\n" + rendered}, nil
	}
	solution, err := board.Solve()
	if err != nil {
		return gameResult{}, errs.New(errs.Usage, "%s", err)
	}
	rendered := board.Render(solution)
	return gameResult{Solution: solution, Board: rendered, text: rendered}, nil
}

func describeQueensStep(step games.Step) string {
	text := step.Reason + "."
	// The reason says where a deduced queen goes; the last guess doesn't.
	if len(step.Place) > 1 || (len(step.Place) == 1 && !strings.Contains(step.Reason, step.Place[0].String())) {
		text += " Queens at " + cellList(step.Place) + "."
	}
	if len(step.Place) == 0 && len(step.Cross) > 0 {
		text += " Cross out " + cellList(step.Cross) + "."
	}
	return text
}

func cellList(cells []games.Cell) string {
	names := make([]string, len(cells))
	for i, c := range cells {
		names[i] = c.String()
	}
	return strings.Join(names, "; ")
}

func sudokuResult(grid *games.Sudoku, hint, steps bool) (gameResult, error) {
	switch {
	case hint:
		step, err := grid.Hint()
		if err != nil {
			return gameResult{}, errs.New(errs.Usage, "%s", err)
		}
		text := step.Reason + "."
		if step.Digit != 0 {
			text += fmt.Sprintf(" Write %d at %s.", step.Digit, step.Cell)
		}
		return gameResult{Hint: step, text: text}, nil
	case steps:
		all, err := grid.Steps()
		if err != nil {
			return gameResult{}, errs.New(errs.Usage, "%s", err)
		}
		solution, err := grid.Solve()
		if err != nil {
			return gameResult{}, errs.New(errs.Usage, "%s", err)
		}
		var lines []string
		for i, step := range all {
			lines = append(lines, fmt.Sprintf("%d. %s: %d. %s.", i+1, step.Cell, step.Digit, step.Reason))
		}
		rendered := grid.RenderGrid(solution)
		return gameResult{Steps: all, Board: rendered, text: strings.Join(lines, "\n") + "\n\n" + rendered}, nil
	}
	solution, err := grid.Solve()
	if err != nil {
		return gameResult{}, errs.New(errs.Usage, "%s", err)
	}
	rendered := grid.RenderGrid(solution)
	return gameResult{Solution: solution, Board: rendered, text: rendered}, nil
}

func (a *app) mcpGamesTools() []mcp.Tool {
	mode := mcp.Param{Name: "mode", Type: "string", Enum: []string{"solution", "hint", "steps"}, Default: "hint",
		Description: "solution: the solved board; hint: the next step a person would take, and why; steps: every step with its reason"}
	return []mcp.Tool{
		{
			Name:  "solve_queens",
			Title: "Queens puzzle helper",
			Description: "Solve LinkedIn's Queens puzzle, or explain the next step like a person would. Offline: it never plays on LinkedIn. " +
				"Give the board one row per line, one letter per cell naming its color region.",
			ReadOnly: true,
			Params: []mcp.Param{
				{Name: "board", Type: "string", Required: true, Description: "the board, one row per line, a letter per cell for its region"},
				mode,
				{Name: "queens", Type: "string", Description: `for a hint, the queens already placed, as ROW,COLUMN from 1: "3,3 1,4"`},
			},
			Run: func(_ context.Context, args mcp.Args) (any, error) {
				board, err := games.ParseQueens(args.String("board"))
				if err != nil {
					return nil, err
				}
				return queensResult(board, args.String("mode") == "hint", args.String("mode") == "steps", args.String("queens"))
			},
		},
		{
			Name:        "solve_sudoku",
			Title:       "Mini Sudoku helper",
			Description: "Solve LinkedIn's Mini Sudoku (6×6) or a classic 9×9 one, or explain the next digit like a person would. Offline: it never plays on LinkedIn.",
			ReadOnly:    true,
			Params: []mcp.Param{
				{Name: "board", Type: "string", Required: true, Description: "the grid, one row per line, a digit per cell and a dot for empty ones"},
				mode,
			},
			Run: func(_ context.Context, args mcp.Args) (any, error) {
				grid, err := games.ParseSudoku(args.String("board"))
				if err != nil {
					return nil, err
				}
				return sudokuResult(grid, args.String("mode") == "hint", args.String("mode") == "steps")
			},
		},
	}
}
