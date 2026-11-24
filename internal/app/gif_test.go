package app

import (
	"bytes"
	"context"
	"errors"
	"image/gif"
	"testing"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
)

func TestGIFPositionsIncludeCaptureCastleAndPromotion(t *testing.T) {
	for _, tc := range []struct {
		name, fen string
		moves     []string
	}{
		{"capture", game.StartFEN, []string{"e2e4", "d7d5", "e4d5"}},
		{"en passant", "7k/8/8/3pP3/8/8/8/7K w - d6 0 1", []string{"e5d6"}},
		{"castle", "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", []string{"e1h1"}},
		{"promotion", "7k/P7/8/8/8/8/8/7K w - - 0 1", []string{"a7a8n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := game.Replay(tc.fen, tc.moves, false)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := RenderGIF(t.Context(), g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := gif.DecodeAll(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte("Cburnett")) || !bytes.Contains(encoded, []byte("creativecommons.org/licenses/by-sa/3.0")) {
				t.Fatal("downloaded artwork lost attribution")
			}
			if len(decoded.Image) != len(tc.moves)+1 || decoded.Config.Width != gifSize || decoded.Config.Height != gifSize {
				t.Fatal("GIF omitted initial or played positions")
			}
			for i, frame := range decoded.Image {
				board := g.History[i]
				for sq, piece := range board.Piece {
					x, y := sq%8, 7-sq/8
					// Inspect the sprite center against its embedded piece image at
					// the corresponding background. This checks decoded piece placement.
					background := uint8((x + y) % 2)
					if i > 0 {
						move := g.Moves[i-1].UCI
						if chess.Sq(sq).String() == move[:2] || chess.Sq(sq).String() == move[2:4] {
							background += 2
						}
					}
					if check, _ := board.IsCheckOrMate(); check && piece == chess.Piece(board.SideToMove|chess.King) {
						background = 4
					}
					want := gifPalette[background]
					if piece != chess.NoPiece {
						want = gifSprites[piece][background].At(gifSquare/2, gifSquare/2)
					}
					got := frame.At(x*gifSquare+gifSquare/2, y*gifSquare+gifSquare/2)
					if got != want {
						t.Fatalf("frame %d square %s has wrong piece pixels", i, chess.Sq(sq))
					}
				}
			}
		})
	}
}

func TestGIFCancellationAndBudgets(t *testing.T) {
	g, err := game.New(game.StartFEN, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RenderGIF(ctx, g); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled render: %v", err)
	}
	tooLong := *g
	tooLong.Moves = make([]game.Move, maxGIFMoves+1)
	if _, err := RenderGIF(t.Context(), &tooLong); !errors.Is(err, errGIFLimit) {
		t.Fatalf("oversized game not rejected: %v", err)
	}
	for range cap(gifSlots) {
		gifSlots <- struct{}{}
	}
	_, err = RenderGIF(t.Context(), g)
	for range cap(gifSlots) {
		<-gifSlots
	}
	if !errors.Is(err, errGIFBusy) {
		t.Fatalf("concurrency bound not enforced: %v", err)
	}
	writer := &gifWriter{ctx: t.Context()}
	writer.buf.Grow(maxGIFBytes)
	_, _ = writer.buf.Write(make([]byte, maxGIFBytes))
	if _, err := writer.Write([]byte{0}); !errors.Is(err, errGIFLimit) {
		t.Fatalf("output bound not enforced: %v", err)
	}
}

func TestGIFMaximumFrameBudgetKeepsEveryPosition(t *testing.T) {
	g, err := game.New(game.StartFEN, false)
	if err != nil {
		t.Fatal(err)
	}
	// Repeat a snapshot to exercise the renderer's largest permitted frame set
	// independently of game adjudication ending a long repetition sequence.
	g.Moves = make([]game.Move, maxGIFMoves)
	g.History = make([]*chess.Board, maxGIFMoves+1)
	for i := range g.History {
		g.History[i] = g.Board
	}
	encoded, err := RenderGIF(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != maxGIFMoves+1 || len(encoded) > maxGIFBytes {
		t.Fatal("maximum export silently truncated or exceeded output budget")
	}
}
