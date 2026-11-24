# Third-party notices

The project license does not replace the separate artwork and dependency
licenses below. Runtime container copies are in `/usr/share/licenses/chess/`.
The original RazChess snapshot inspected for compatibility is
`razzie/razchess@1da855236570ca9f56162b71e854bf4c37ea1ad5`.

## Browser libraries

| Files under `internal/web/static/` | Attribution and license |
| --- | --- |
| `jquery-3.6.3.min.js` | OpenJS Foundation and other contributors; MIT. Exact upstream notice: `licenses/jquery-3.6.3.txt`, from `jquery/jquery` tag `3.6.3`, `LICENSE.txt`. |
| `chessboard-1.0.0.min.js`, `chessboard-1.0.0.min.css` | Copyright 2019 Chris Oakman; MIT. Exact upstream notice: `licenses/chessboardjs-1.0.0.txt`, from `oakmac/chessboardjs` tag `v1.0.0` (`bfa31a05da24e6c3877f7acfa12dfa77ad7638bf`), `LICENSE.md`. |
| `htmx.min.js` | Big Sky Software; Zero-Clause BSD. The complete supplied notice is `HTMX-LICENSE` beside the asset and `licenses/HTMX-LICENSE`. |
| `pico.min.css` | Pico CSS authors; MIT. The complete supplied notice is `PICO-LICENSE.md` beside the asset and `licenses/PICO-LICENSE.md`. |

The supplied local Razzie Cloud logos and shared account-header styles are
first-party platform assets copied from the parent/sibling services.

## Chess-piece artwork

The twelve `pieces/*.png` files depict the standard chess-piece designs by
[Cburnett](https://commons.wikimedia.org/wiki/User:Cburnett) on Wikimedia Commons.
They are used under [CC BY-SA 3.0 Unported](https://creativecommons.org/licenses/by-sa/3.0/),
with the complete license retained in `licenses/CC-BY-SA-3.0.txt`. Individual
Commons design pages and the checked metadata/hashes are recorded in
`docs/artwork-provenance.json`.

| PNG | Original design page |
| --- | --- |
| `wP.png`, `bP.png` | [White pawn](https://commons.wikimedia.org/wiki/File:Chess_plt45.svg), [Black pawn](https://commons.wikimedia.org/wiki/File:Chess_pdt45.svg) |
| `wN.png`, `bN.png` | [White knight](https://commons.wikimedia.org/wiki/File:Chess_nlt45.svg), [Black knight](https://commons.wikimedia.org/wiki/File:Chess_ndt45.svg) |
| `wB.png`, `bB.png` | [White bishop](https://commons.wikimedia.org/wiki/File:Chess_blt45.svg), [Black bishop](https://commons.wikimedia.org/wiki/File:Chess_bdt45.svg) |
| `wR.png`, `bR.png` | [White rook](https://commons.wikimedia.org/wiki/File:Chess_rlt45.svg), [Black rook](https://commons.wikimedia.org/wiki/File:Chess_rdt45.svg) |
| `wQ.png`, `bQ.png` | [White queen](https://commons.wikimedia.org/wiki/File:Chess_qlt45.svg), [Black queen](https://commons.wikimedia.org/wiki/File:Chess_qdt45.svg) |
| `wK.png`, `bK.png` | [White king](https://commons.wikimedia.org/wiki/File:Chess_klt45.svg), [Black king](https://commons.wikimedia.org/wiki/File:Chess_kdt45.svg) |

The delivered 80×80 PNG renderings are byte-identical to the Wikipedia set in
`oakmac/chessboardjs@bfa31a05da24e6c3877f7acfa12dfa77ad7638bf`, also copied by
the pinned original RazChess. Chess changes their directory/name only; no image
pixels were modified. Rasterization predates this project. CC BY-SA applies to
the artwork and adaptations, including its appearance in exported GIFs; preserve
this attribution/license when redistributing those assets or adapted artwork.
No endorsement by the artwork author is implied.

## Sound feedback

`sounds/*.ogg` are new original synthesized tones, generated from sine waves
by `tools/generate-sounds.go` and encoded locally with FFmpeg/libvorbis. They
contain no sampled audio. The project license applies to these tones and their
generator; FFmpeg is a development tool, not a runtime dependency.

The old RazChess samples were introduced by commit
`b383d18db7e5f4ec7af9536ffbaccfbc4d04b9c5` ("Added sounds"). Its history and
encoder metadata supplied no independent recording attribution or per-file
license. None of those recordings is retained or redistributed here. New tones
preserve feedback categories without assuming rights over unknown recordings.

## Openings and Go dependencies

The complete legacy ECO/Lichess source and normalization are documented in
`docs/rules-compatibility.md`. The opening data uses CC0 1.0, retained in
`licenses/lichess-openings-CC0.txt`; the original notnil/chess and RazChess MIT
notices are retained in `licenses/notnil-chess.txt` and `licenses/razchess.txt`.
The old rules libraries are not runtime dependencies.

`go.mod`/`go.sum` pin the runtime dependencies. Exact available upstream license
files for every module returned by `go list -deps ./cmd/chess` are retained
under `licenses/go/`, with module/version inventory in `licenses/go/modules.tsv`.
The Go standard-library notice is `licenses/go-stdlib.txt`. The selected
malbrecht/chess MIT notice is additionally available at
`licenses/malbrecht-chess.txt`. Refresh the license inventory when changing
runtime modules.
