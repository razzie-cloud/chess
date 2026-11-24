package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"io/fs"
	"net/http"
	"sync"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/web"
)

const (
	gifSquare   = 32
	gifSize     = 8 * gifSquare
	maxGIFMoves = 512
	maxGIFBytes = 32 << 20
)

var (
	errGIFLimit = errors.New("GIF export supports at most 512 moves and 32 MiB. Export the complete game as PGN instead.")
	errGIFBusy  = errors.New("GIF rendering is busy. Please try again shortly.")
	gifSlots    = make(chan struct{}, 2)
	gifOnce     sync.Once
	gifSprites  map[chess.Piece][]*image.Paletted
	gifLoadErr  error
	gifPalette  = color.Palette{
		color.RGBA{238, 238, 210, 255}, color.RGBA{118, 150, 86, 255},
		color.RGBA{246, 246, 135, 255}, color.RGBA{186, 202, 68, 255},
		color.RGBA{225, 101, 91, 255},
	}
)

func loadGIFSprites() {
	for i := range 64 {
		v := uint8(i * 255 / 63)
		gifPalette = append(gifPalette, color.RGBA{v, v, v, 255})
	}
	gifSprites = make(map[chess.Piece][]*image.Paletted)
	for name, piece := range map[string]chess.Piece{
		"wP": chess.WP, "wN": chess.WN, "wB": chess.WB, "wR": chess.WR, "wQ": chess.WQ, "wK": chess.WK,
		"bP": chess.BP, "bN": chess.BN, "bB": chess.BB, "bR": chess.BR, "bQ": chess.BQ, "bK": chess.BK,
	} {
		data, err := fs.ReadFile(web.Static(), "pieces/"+name+".png")
		if err != nil {
			gifLoadErr = err
			return
		}
		pieceImage, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			gifLoadErr = err
			return
		}
		for background := range 5 {
			canvas := image.NewRGBA(image.Rect(0, 0, gifSquare, gifSquare))
			draw.Draw(canvas, canvas.Bounds(), image.NewUniform(gifPalette[background]), image.Point{}, draw.Src)
			// Precompose each embedded sprite on each board color once. Every frame
			// can then copy palette indices without per-pixel color matching.
			scaled := image.NewRGBA(canvas.Bounds())
			bounds := pieceImage.Bounds()
			for y := range gifSquare {
				for x := range gifSquare {
					scaled.Set(x, y, pieceImage.At(bounds.Min.X+x*bounds.Dx()/gifSquare, bounds.Min.Y+y*bounds.Dy()/gifSquare))
				}
			}
			draw.Draw(canvas, canvas.Bounds(), scaled, image.Point{}, draw.Over)
			tile := image.NewPaletted(canvas.Bounds(), gifPalette)
			draw.Draw(tile, tile.Bounds(), canvas, image.Point{}, draw.Src)
			gifSprites[piece] = append(gifSprites[piece], tile)
		}
	}
}

func gifFrame(board *chess.Board, last string) *image.Paletted {
	frame := image.NewPaletted(image.Rect(0, 0, gifSize, gifSize), gifPalette)
	check, _ := board.IsCheckOrMate()
	for sq, piece := range board.Piece {
		x, y := sq%8, 7-sq/8
		background := uint8((x + y) % 2)
		coordinate := chess.Sq(sq).String()
		if len(last) >= 4 && (coordinate == last[:2] || coordinate == last[2:4]) {
			background += 2
		}
		if check && piece == chess.Piece(board.SideToMove|chess.King) {
			background = 4
		}
		for row := range gifSquare {
			pixels := frame.Pix[(y*gifSquare+row)*frame.Stride+x*gifSquare:][:gifSquare]
			if piece == chess.NoPiece {
				for col := range pixels {
					pixels[col] = background
				}
			} else {
				tile := gifSprites[piece][background]
				copy(pixels, tile.Pix[row*tile.Stride:][:gifSquare])
			}
		}
	}
	return frame
}

type gifWriter struct {
	ctx context.Context
	buf bytes.Buffer
}

func (w *gifWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > maxGIFBytes-w.buf.Len() {
		return 0, errGIFLimit
	}
	return w.buf.Write(p)
}

// RenderGIF consumes an already-authorized consistent snapshot. The 513-frame
// bound retains at most 33.6 MiB of indexed frame pixels per render; two slots
// bound aggregate frame memory, and output has a separate 32 MiB limit.
func RenderGIF(ctx context.Context, g *game.Game) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil || g.Board == nil || len(g.Moves) > maxGIFMoves || len(g.History) != len(g.Moves)+1 {
		return nil, errGIFLimit
	}
	select {
	case gifSlots <- struct{}{}:
		defer func() { <-gifSlots }()
	default:
		return nil, errGIFBusy
	}
	gifOnce.Do(loadGIFSprites)
	if gifLoadErr != nil {
		return nil, gifLoadErr
	}
	animation := &gif.GIF{LoopCount: 0}
	for i, board := range g.History {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if board == nil {
			return nil, errGIFLimit
		}
		last := ""
		if i > 0 {
			last = g.Moves[i-1].UCI
		}
		animation.Image = append(animation.Image, gifFrame(board, last))
		delay := 60
		if i == len(g.History)-1 {
			delay = 180
		}
		animation.Delay = append(animation.Delay, delay)
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	writer := &gifWriter{ctx: ctx}
	if err := gif.EncodeAll(writer, animation); err != nil {
		return nil, err
	}
	// Keep the embedded artwork's attribution with a downloaded derivative.
	comment := []byte("Chess piece artwork by Cburnett, licensed CC BY-SA 3.0: https://creativecommons.org/licenses/by-sa/3.0/ . Source: https://commons.wikimedia.org/wiki/Category:SVG_chess_pieces . GIF artwork is shared under the same license.")
	writer.buf.Truncate(writer.buf.Len() - 1)
	if _, err := writer.Write(append(append([]byte{0x21, 0xfe, byte(len(comment))}, comment...), 0, 0x3b)); err != nil {
		return nil, err
	}
	return writer.buf.Bytes(), nil
}

func (s *Server) writeGIF(w http.ResponseWriter, r *http.Request, g *game.Game) {
	data, err := RenderGIF(r.Context(), g)
	if err != nil {
		w.Header().Del("Content-Disposition")
		switch {
		case errors.Is(err, errGIFLimit):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		case errors.Is(err, errGIFBusy):
			w.Header().Set("Retry-After", "2")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		default:
			s.fail(w, r, err)
		}
		return
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
