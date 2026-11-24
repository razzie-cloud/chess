package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/store"
)

type PuzzleView struct {
	Collections                    []store.PuzzleCollection
	Collection                     *store.PuzzleCollection
	Attempt                        *store.Attempt
	Branches                       []BranchView
	Slot                           int
	Title, FEN, Solver, Error      string
	Mate                           int
	ActiveBranches, CompletedLines int
}
type BranchView struct {
	store.PuzzleBranch
	Revision                                   int64
	Digest, Solver, AttemptID, PathJSON, Label string
	Legal                                      []string
	Parent                                     bool
	Playable                                   bool
	CheckSquare                                string
}

func (s *Server) puzzleRoutes(p *http.ServeMux) {
	p.HandleFunc("GET /puzzles", s.collections)
	p.HandleFunc("GET /puzzles/{collection}", s.collections)
	p.HandleFunc("GET /puzzles/{collection}/{slot}", s.puzzleDetail)
	p.HandleFunc("POST /puzzles/{collection}/{slot}/attempts", s.startAttempt)
	p.HandleFunc("GET /puzzle-attempts/{id}", s.attemptPage)
	p.HandleFunc("GET /puzzle-attempts/{id}/state", s.attemptState)
	p.HandleFunc("POST /puzzle-attempts/{id}/branches/{branch}/moves", s.puzzleMove)
	p.HandleFunc("POST /puzzle-attempts/{id}/end", s.endAttempt)
	for _, format := range []string{"fen", "pgn", "gif"} {
		p.HandleFunc("GET /puzzle-attempts/{id}/branches/{branch}/export/"+format, s.puzzleExport)
	}
}
func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzles")
	all, e := s.store.Collections(r.Context(), p.User.ID)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	v := PuzzleView{Collections: all}
	if id := r.PathValue("collection"); id != "" {
		for i := range all {
			if all[i].ID == id {
				v.Collection = &all[i]
				p.Title = all[i].Title
				break
			}
		}
		if v.Collection == nil {
			s.fail(w, r, store.ErrDenied)
			return
		}
	}
	p.Puzzle = v
	s.render(w, r, "collections", p, 200)
}
func (s *Server) puzzleDetail(w http.ResponseWriter, r *http.Request) {
	slot, e := strconv.Atoi(r.PathValue("slot"))
	if e != nil {
		s.fail(w, r, store.ErrDenied)
		return
	}
	d, e := s.store.Definition(r.Context(), r.PathValue("collection"), slot)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	p := s.page(r, d.Title)
	p.Base = "/puzzles/" + r.PathValue("collection") + "/" + strconv.Itoa(slot)
	p.Puzzle = PuzzleView{Title: d.Title, FEN: d.FEN, Solver: d.Solver, Mate: d.Mate, Slot: d.Slot}
	s.render(w, r, "puzzle-detail", p, 200)
}
func (s *Server) startAttempt(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzle")
	slot, e := strconv.Atoi(r.PathValue("slot"))
	if e != nil {
		s.fail(w, r, store.ErrDenied)
		return
	}
	a, e := s.store.StartAttempt(r.Context(), p.User.ID, r.PathValue("collection"), slot, r.FormValue("request_id"), r.FormValue("restart") == "true")
	if e != nil {
		s.fail(w, r, e)
		return
	}
	navigate(w, r, "/puzzle-attempts/"+a.ID)
}
func (s *Server) attemptData(r *http.Request, a store.Attempt) Page {
	p := s.page(r, a.Title)
	p.Base = "/puzzle-attempts/" + a.ID
	v := PuzzleView{Attempt: &a, Title: a.Title, Mate: a.Mate, Solver: a.Solver}
	for _, b := range a.Branches {
		branch := BranchView{PuzzleBranch: b, Revision: a.Revision, Digest: a.Digest, Solver: a.Solver, AttemptID: a.ID, Playable: b.Status == "pending" && a.Status == "active"}
		for _, other := range a.Branches {
			if other.ParentID == b.ID {
				branch.Parent = true
				break
			}
		}
		path, _ := json.Marshal(b.Path)
		if b.Path == nil {
			path = []byte("[]")
		}
		branch.PathJSON = string(path)
		if !branch.Parent {
			if b.Status == "pending" && a.Status == "active" {
				v.ActiveBranches++
			}
			if b.Status == "solved" {
				v.CompletedLines++
			}
		}
		if len(b.Path) == 0 {
			branch.Label = "Starting position"
		} else if g, e := game.Replay(a.InitialFEN, b.Path, false); e == nil {
			var labels []string
			for _, m := range g.Moves {
				labels = append(labels, m.SAN)
			}
			branch.Label = strings.Join(labels, " ")
		}
		if board, e := game.ValidateFEN(b.FEN); e == nil {
			if check, _ := board.IsCheckOrMate(); check {
				for square, piece := range board.Piece {
					if piece.Type() == chess.King && piece.Color() == board.SideToMove {
						branch.CheckSquare = chess.Sq(square).String()
						break
					}
				}
			}
			if b.Status == "pending" && a.Status == "active" {
				for _, m := range board.LegalMoves() {
					branch.Legal = append(branch.Legal, m.Uci(board))
				}
			}
		}
		v.Branches = append(v.Branches, branch)
	}
	p.Puzzle = v
	return p
}
func (s *Server) attemptPage(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzle")
	a, e := s.store.Attempt(r.Context(), r.PathValue("id"), p.User.ID)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	s.render(w, r, "attempt", s.attemptData(r, a), 200)
}
func (s *Server) attemptState(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzle")
	a, e := s.store.Attempt(r.Context(), r.PathValue("id"), p.User.ID)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	s.render(w, r, "attempt-state", s.attemptData(r, a), 200)
}
func (s *Server) puzzleMove(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzle")
	rev, e := strconv.ParseInt(r.FormValue("revision"), 10, 64)
	if e != nil {
		s.fail(w, r, store.ErrInvalid)
		return
	}
	var path []string
	if e = json.Unmarshal([]byte(r.FormValue("path")), &path); e != nil {
		s.fail(w, r, store.ErrInvalid)
		return
	}
	a, e := s.store.PuzzleMove(r.Context(), r.PathValue("id"), p.User.ID, store.PuzzleMove{Branch: r.PathValue("branch"), ContentDigest: r.FormValue("content_digest"), Path: path, UCI: r.FormValue("uci"), Revision: rev})
	if e != nil {
		s.fail(w, r, e)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, r, "attempt-state", s.attemptData(r, a), 200)
	} else {
		navigate(w, r, "/puzzle-attempts/"+a.ID)
	}
}
func (s *Server) endAttempt(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzle")
	rev, e := strconv.ParseInt(r.FormValue("revision"), 10, 64)
	if e != nil {
		s.fail(w, r, store.ErrInvalid)
		return
	}
	a, e := s.store.EndAttempt(r.Context(), r.PathValue("id"), p.User.ID, r.FormValue("request_id"), rev)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	navigate(w, r, "/puzzle-attempts/"+a.ID)
}
func (s *Server) puzzleExport(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzle")
	a, e := s.store.Attempt(r.Context(), r.PathValue("id"), p.User.ID)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	b, e := a.PlayedBranch(r.PathValue("branch"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	g, e := game.Replay(a.InitialFEN, b.Path, false)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	format := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="puzzle.%s"`, format))
	switch format {
	case "fen":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, b.FEN+"\n")
	case "pgn":
		w.Header().Set("Content-Type", "application/x-chess-pgn")
		_, _ = io.WriteString(w, game.ExportPGN(g))
	case "gif":
		s.writeGIF(w, r, g)
	}
}
