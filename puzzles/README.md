# Prepared mate collections

`source.json` retains every original RazChess position at its permanent original
zero-based source slot. The imported source is
[`pkg/razchess/puzzles.go`](https://github.com/razzie/razchess/blob/1da855236570ca9f56162b71e854bf4c37ea1ad5/pkg/razchess/puzzles.go),
commit `1da855236570ca9f56162b71e854bf4c37ea1ad5`. The original repository's MIT
notice is retained in `../licenses/razchess.txt`. That repository records no
individual composition authors; this limitation is retained in each entry's
provenance rather than inventing attribution. `legacy-razchess.fen` is its
unmodified position sequence, extracted from those quoted source strings.

The chosen first edition, `razchess-mates-v1`, declares a maximum of three solver
moves. Of 220 positions, 207 publish and 13 remain explicit unpublished drafts.
There are 138 single-line puzzles, 60 with two complete lines, and nine with
three complete lines. No published strategy has more than one fork; the largest
published proof contains five solver decisions across all its lines. Original
slots 50, 66, 70, 77, 98, 107, 156, 162, 174, 180, 198, 203 and 208 remain drafts.
Nothing renumbers the gaps or weakens the defense-coverage rule.

The preparation prototype screened all 220 positions with explicit mate limits
2, 3, 4 and 5, using 100,000 search nodes and two seconds per entry:

| Mate limit | Accepted | Rejected | Largest search node count |
| --- | ---: | ---: | ---: |
| 2 | 206 | 14 | 393 |
| 3 | 207 | 13 | 1,414 |
| 4 | 207 | 13 | 4,566 |
| 5 | 207 | 13 | 11,685 |

`legacy-screening-mate-N.json` records every acceptance/rejection with a position,
candidate path, counts and measured resource usage. These are bounded constrained
search results, not statements that rejected positions have no mate in ordinary
chess. The rejection diagnostic includes an illustrative candidate that violates
the defense bound. No entry exhausted the stated node/time budget. A search
budget failure is always reported as unproven.

Compatibility fixtures include forced single-line slot 0, two-reply fork slot 9,
three-reply fork slot 35 and the mate-in-three, three-line slot 15. Every bundled
solution is replayed independently: all legal defenses must appear, every leaf
must be checkmate, and global fork/line counts are recomputed. The runtime never
searches and never receives a remaining solution from a browser.

## Onboarding

Add writes a draft with a permanent slot and explicit mate limit:

```sh
go run ./cmd/puzzleprep add -manifest puzzles/source.json -collection my-mates-v1 -slot 0 -mate 3 -fen '<six-field FEN>' -title 'My puzzle' -provenance '<source>' -license '<license>'
```

`import` accepts newline-FEN input, the same metadata and a starting slot. It
rejects conflicting slots and more than 256 source positions. Neither command
selects entries for publication. Review the draft's metadata and set `publish`
explicitly before compilation. FEN-only input never supplies an inferred mate
depth.

```sh
go run ./cmd/puzzleprep compile -input puzzles/source.json -output internal/db/seeds/puzzles.json -report puzzles/preparation-report.json
go run ./cmd/puzzleprep verify -input internal/db/seeds/puzzles.json
```

Compilation reports all selected entries and drafts. Any selected failure exits
nonzero and preserves the previous seed and source manifests. Output replacement
uses a same-directory temporary file and rename. Successful seed JSON is
deterministic; elapsed times live only in reports. CI/tests recompile the selected
entries and compare the bundle. Defaults cap depth at five solver moves, 500,000
search nodes, five seconds per entry and 2 MiB per compiled puzzle. Terminal
draws, repetitions and the 75-move rule follow the application rules.

Changing a published position, solver, mate objective or solution requires a new
collection edition. Metadata title corrections are allowed. Startup validates
and imports embedded seeds under a PostgreSQL transaction advisory lock, compares
immutable digests, and fails on a conflicting existing slot. Concurrent/repeated
imports do not duplicate records or overwrite progress; omitted editions remain
in the database. No content tool connects to a database or network.

Personal progress uses PostgreSQL `BIT(256)` and atomic `set_bit` updates. Attempt
completion and the bit update commit together after every required branch mates.
Restarting/ending attempts and repeated solves never clear bits. Branch histories
remain distinct even when positions transpose; transition receipts are keyed by
attempt, branch and canonical source path, including automatic defense moves.
