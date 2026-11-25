# Prepared mate collections

`source.json` contains the 206 supported positions from the original RazChess
collection, numbered consecutively at slots 0–205 (shown as puzzles 1–206).
Every entry is a calculated mate in two. Fourteen original positions are excluded
because their shortest mating strategies cannot satisfy the single-fork,
three-board limit. The original index remains in each entry's provenance;
`legacy-razchess.fen` retains all 220 original positions.

The imported source is
[`pkg/razchess/puzzles.go`](https://github.com/razzie/razchess/blob/1da855236570ca9f56162b71e854bf4c37ea1ad5/pkg/razchess/puzzles.go),
commit `1da855236570ca9f56162b71e854bf4c37ea1ad5`. Its MIT notice is retained in
`../licenses/razchess.txt`. Individual composition authors were not recorded;
provenance preserves that limitation rather than inventing attribution.

The collection is `razchess-mates-v1`: 138 single-line puzzles, 60 with two lines
and eight with three lines. Original indices 15, 50, 66, 70, 77, 98, 107, 156,
162, 174, 180, 198, 203 and 208 are excluded. Original index 15 has a mate in two
with 27 defenses; its previously selected three-move strategy is no longer used.
This renumbering replaces the unreleased seed and resets local puzzle progress.
Once published, collection slots and solutions remain immutable.

The historical preparation prototype screened all 220 positions with explicit
mate limits 2, 3, 4 and 5, using 100,000 search nodes and two seconds per entry:

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

Compatibility fixtures include forced single-line slot 0, two-reply fork slot 9
and three-reply fork slot 34. The excluded original index 15 is a regression fixture
for rejecting longer solutions when the shortest mate exceeds the board limit.
Every bundled solution is replayed independently: all legal defenses must appear, every leaf
must be checkmate, and global fork/line counts are recomputed. The runtime never
searches and never receives a remaining solution from a browser.

Gameplay also accepts any legal immediate checkmate at a solver decision,
including a different mating piece or an earlier mate. The actual move and
resulting position are saved; other defense branches still require completion.
Non-mating moves must match the prepared solution. This runtime rule does not
change published definitions, their digests or existing progress.

## Onboarding

Add and import calculate the shortest forced mate, then write unpublished drafts:

```sh
go run ./cmd/puzzleprep add -manifest puzzles/source.json -collection my-mates-v1 -slot 0 -fen '<six-field FEN>' -title 'My puzzle' -provenance '<source>' -license '<license>'
```

`import` accepts newline-FEN input, the same metadata and a starting slot. It
rejects conflicting slots and more than 256 source positions. Neither command
selects entries for publication. Review the draft's metadata and set `publish`
explicitly before compilation. Search defaults to at most five solver moves;
optional `-mate N` lowers that search ceiling. The stored `mate` is the calculated
length, not the ceiling. Unproven or budget-exhausted input fails without replacing
the manifest. Publication additionally requires a shortest strategy that fits
the board limit; compilation never substitutes a longer mating strategy.

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
