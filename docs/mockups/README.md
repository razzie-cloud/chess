# Chess custom game proposal

Open [light](custom-game.html) or [dark](dark/custom-game.html) through the parent
preview server: `go run scripts/serve.go`, then visit
`http://127.0.0.1:8765/chess/docs/mockups/custom-game.html`.

The desktop layout places the board and its tools on the left, with position
source and match details on the right. Board editor, FEN and PGN are mutually
exclusive source panels; switching keeps their drafts. Side to move stays visible
in the editor; castling, en passant and move counters live in **Position rules**.
The production header and horizontal Chess navigation stay familiar. Inactive
navigation labels show context; only Custom game is included in the preview.
Tablet stacks the board above two settings columns; phones use one column.

Try dragging spare pieces, removing pieces, Default position, Clear board, Flip
board, position rules, FEN preview and switching sources. FEN format errors retain
the entered text. Format checks do not establish legal positions. PGN is retained
as a draft and is not replayed. Create match loads an authored HTMX receipt; it
never creates a game or link and never submits form values. Account management
uses an intentionally invalid example destination. The preview has no backend.

Screenshots: [desktop](custom-game-desktop.png),
[mobile](custom-game-mobile.png), [dark desktop](dark/custom-game-desktop.png),
[dark mobile](dark/custom-game-mobile.png).

Verification (2026-11-26): Brave/Chromium passed both themes at 320, 390, 768 and
1440 pixels, including overflow, loaded images, one primary heading, FEN format
success/failure, retained PGN drafts, rule changes, clear/reset/flip, keyboard
source selection, focus, menu open/Escape, theme counterparts and simulated
creation. All 244 requests were local GETs without query values; there were no
failed responses or console/CSP errors. Four screenshots were saved and desktop
and mobile views visually inspected. Local HTML references and Git whitespace
checks passed. No production assets changed, so service build/tests were not run.
No live SSO, legal-position validation or PGN replay was tested.

Production now implements this layout, with live import validation, and gives
normal/Chess960 creators matching board previews and match details. The static
proposal remains an independent fixture. See [creator verification](../verification.md#match-creators--26-november-2026)
and the [production normal-game screenshot](../screenshots/setup-standard-light-1440.png).
