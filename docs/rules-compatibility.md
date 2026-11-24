# Rules and opening compatibility

The runtime rules dependency is `github.com/malbrecht/chess` at
`3465f589170a` (module version
`v0.0.0-20190911134141-3465f589170a`). It builds with Go 1.27.4. Its MIT
license is retained in `licenses/malbrecht-chess.txt`.

`internal/game` applies a strict six-field FEN validator and strict coordinate
UCI boundary before the library. Canonical castling is king-to-own-rook UCI;
ordinary chess also accepts king-destination input. Chess960 accepts only the
canonical gesture. Fixtures cover all 960 Scharnagl starts, normal castling,
stationary king and stationary rook castling, promotions, en passant and pins.
Application adjudication adds checkmate, stalemate, fivefold repetition, the
75-move rule and the established insufficient-material cases. Checkmate wins
over the move-count draw. Repetition ignores counters and en-passant targets
without a legal en-passant capture, including pinned captures.

PGN import is bounded to 256 KiB, 2,048 mainline plies, 64 tags (2,048 bytes
per value), 128 bytes per token and 16 nested variations. It scans comments
and variations explicitly and imports one mainline. A final result marker is
mandatory and must agree with the Result tag and any automatic outcome.
Nonterminal reported results are retained as `imported_result`. Export preserves
setup/variant/result metadata and Black-to-move numbering. It does not recover
history before an imported FEN or prove that arbitrary positions are reachable.

## ECO data provenance

The source is the complete `opening/eco_lichess.tsv` vendored by RazChess commit
`1da855236570ca9f56162b71e854bf4c37ea1ad5` (9 November 2026), from its pinned
`github.com/razzie/chess` fork `5022223cc522`. Its opening README identifies
[Lichess chess-openings](https://github.com/lichess-org/chess-openings) as the
upstream dataset. Lichess publishes it under
[CC0 1.0](https://github.com/lichess-org/chess-openings/blob/master/COPYING.txt);
the complete dedication is in `licenses/lichess-openings-CC0.txt`. The original
notnil/chess MIT notice is also preserved in `licenses/notnil-chess.txt`.

The exact source TSV is retained in `internal/opening/source/eco.tsv`, SHA-256
`d05e09bea1b452069a4fd6fd56bf5d23ebe13d3a9abf50b4bec7b5030559fcf9`.
`go generate ./internal/opening` replays all 3,397 records through the selected
rules library and writes deterministic `internal/opening/eco.tsv`. Every source
line is accepted; no opening is discarded. Castling is normalized from the
source's king-destination notation to canonical king-to-rook UCI. Preparation
fails on any malformed record and preserves the previous output.

The runtime uses one immutable prefix trie. It preserves source-row order and
the original book's last-row precedence where labels share a move sequence.
Lookup retains the deepest title after leaving book, derives it afresh from
durable canonical moves and suppresses labels for Chess960/custom starts.
This is move-order recognition, without transposition matching. Tests regenerate
the entire artifact and compare bytes, exercise deeper labels/book departure,
PGN imports and reconstructed histories. No original rules package is used at
runtime or during normalization.
