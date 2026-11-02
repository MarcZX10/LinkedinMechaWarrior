# LinkedIn's puzzles

> **Short version:** type the day's Queens board into
> `lmw games queens --hint` and `lmw` tells you the next step and why, the
> way a person would think it through. `--steps` explains every step,
> and without either it shows the solution. `lmw games sudoku` does the
> same for Mini Sudoku.

You play; `lmw` thinks along. These commands work offline: they never open
LinkedIn's games, never play for you and send no request at all, so your
streak and your place among your connections stay yours.

## Queens

Write the board one row per line, one letter per cell for its color: the
same letter for the same region. Spaces don't matter.

```text
A A B B B
A A B B B
C C Z B B
C C D D D
C C C C D
```

```bash
lmw games queens board.txt --hint
```

```text
Region Z has only one free cell left, so its queen goes at row 3, column 3.
```

Placed some queens already? Say where, as `ROW,COLUMN` counted from 1, and
the hint starts from there:

```bash
lmw games queens board.txt --hint --queens "3,3"
```

```text
Region D fits only in column 5, so that column's queen is its own: no other region can have a queen there. Cross out row 1, column 5; row 2, column 5.
```

`--steps` walks the whole way, then draws the solved board (`Q` for the
queens, the region letters elsewhere):

```bash
lmw games queens board.txt --steps
```

```text
1. Region Z has only one free cell left, so its queen goes at row 3, column 3.
2. Region D fits only in column 5, so that column's queen is its own: no other region can have a queen there. Cross out row 1, column 5; row 2, column 5.
3. Row 2 has only one free cell left, so its queen goes at row 2, column 1.
4. Row 1 has only one free cell left, so its queen goes at row 1, column 4.
5. Row 4 has only one free cell left, so its queen goes at row 4, column 5.
6. Row 5 has only one free cell left, so its queen goes at row 5, column 2.

a a b Q b
Q a b b b
c c Q b b
c c d d Q
c Q c c d
```

The reasoning, simplest first:

1. **A lone cell.** A row, column or region with one free cell left gets its
   queen there.
2. **Regions that fill rows.** When some regions fit only in as many rows
   (or columns), those rows' queens are theirs, so no other region can have
   a queen there. And the other way round: rows that can only take queens
   from as many regions leave those regions nothing elsewhere.
3. **A queen that blocks.** A queen on a cell that would leave some row,
   column or region with no free cell can't go there.
4. **Following it through.** Putting a queen on a cell and following the
   moves it forces ("then row 2's queen must go here...") until something is
   left with no room shows that cell is wrong.

If none of these finds the next step, `lmw` says so; the solution comes
from trying queens until one fits.

## Mini Sudoku

Write the grid one row per line, a digit per cell and a dot for empty ones.
Box lines (`|`, `-`, `+`) are fine and ignored. Mini Sudoku is 6×6 with
boxes of two rows by three columns; a classic 9×9 sudoku works too.

```text
1 . . | . 5 6
. 5 6 | 1 . .
------+------
2 . . | . 6 4
. 6 4 | 2 . .
------+------
3 . . | . 4 5
. 4 5 | 3 . .
```

```bash
lmw games sudoku grid.txt --hint
```

The hint is one of two steps a person looks for: a cell where every other
digit is already in its row, column or box, or a row, column or box with
only one cell left for a digit it needs. `--steps` lists them all, and
without either flag you get the solved grid.

## Without a file

Leave out the file and type or paste the board, ending it with an empty
line. That works well with the clipboard:

```bash
lmw games queens --hint
```

## From your assistant

`solve_queens` and `solve_sudoku` give the same hints to an AI assistant
through [MCP](ai-assistants.md): describe or paste the board and ask
"what's my next move, and why?". Like the commands, they work offline.
