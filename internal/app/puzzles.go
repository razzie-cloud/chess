package app

import (
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/puzzle"
	"github.com/razzie-cloud/chess/internal/store"
)

type PuzzleView struct {
	Collections                                  []store.PuzzleCollection
	Collection                                   *store.PuzzleCollection
	State                                        puzzle.PlayState
	Branches                                     []BranchView
	Slot, Mate                                   int
	Title, Solver, Digest, CollectionID, NextURL string
	Done                                         bool
	ActiveBranches, CompletedLines               int
}
type BranchView struct {
	puzzle.Board
	Solver, Base, PathJSON, Label, CheckSquare, Turn string
	Legal                                            []string
}

func (s *Server) puzzleRoutes(p *http.ServeMux) {
	p.HandleFunc("GET /puzzles", s.collections)
	p.HandleFunc("GET /puzzles/random", s.randomPuzzle)
	p.HandleFunc("GET /puzzles/{collection}", s.collections)
	p.HandleFunc("GET /puzzles/{collection}/{slot}", s.puzzleDetail)
	p.HandleFunc("POST /puzzles/{collection}/{slot}/moves", s.puzzleMove)
}
func (s *Server) randomPuzzle(w http.ResponseWriter, r *http.Request) {
	collections, err := s.store.Collections(r.Context(), s.page(r, "Puzzles").User.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	total := 0
	for _, collection := range collections {
		total += len(collection.Slots)
	}
	if total == 0 {
		http.Redirect(w, r, "/puzzles", http.StatusSeeOther)
		return
	}
	choice := rand.IntN(total)
	for _, collection := range collections {
		if choice < len(collection.Slots) {
			dest := "/puzzles/" + collection.ID + "/" + strconv.Itoa(collection.Slots[choice].Slot)
			http.Redirect(w, r, dest, http.StatusSeeOther)
			return
		}
		choice -= len(collection.Slots)
	}
}
func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Puzzles")
	all, err := s.store.Collections(r.Context(), p.User.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := PuzzleView{Collections: all}
	if id := r.PathValue("collection"); id != "" {
		for i := range all {
			if all[i].ID == id {
				v.Collection = &all[i]
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
func (s *Server) puzzleData(r *http.Request, d puzzle.Definition, state puzzle.PlayState) (Page, error) {
	p := s.page(r, d.Title)
	collection := r.PathValue("collection")
	p.Base = "/puzzles/" + collection + "/" + strconv.Itoa(d.Slot)
	v := PuzzleView{Title: d.Title, Mate: d.Mate, Solver: d.Solver, Digest: d.Digest, Slot: d.Slot, CollectionID: collection, State: state}
	all, err := s.store.Collections(r.Context(), p.User.ID)
	if err != nil {
		return p, err
	}
	for _, c := range all {
		if c.ID != collection {
			continue
		}
		// Prefer later puzzles; wrap to the first remaining undone puzzle.
		for _, slot := range c.Slots {
			if slot.Slot == d.Slot {
				v.Done = slot.Solved
				continue
			}
			if slot.Solved {
				continue
			}
			if v.NextURL == "" || slot.Slot > d.Slot {
				v.NextURL = "/puzzles/" + collection + "/" + strconv.Itoa(slot.Slot)
				if slot.Slot > d.Slot {
					break
				}
			}
		}
	}
	for _, b := range state.Boards {
		branch := BranchView{Board: b, Solver: d.Solver, Base: p.Base}
		path, _ := json.Marshal(b.Path)
		branch.PathJSON = string(path)
		if b.Done {
			v.CompletedLines++
		} else {
			v.ActiveBranches++
		}
		if len(b.Path) == 0 {
			branch.Label = "Starting position"
		} else if g, e := game.Replay(d.FEN, b.Path, false); e == nil {
			var labels []string
			for _, m := range g.Moves {
				labels = append(labels, m.SAN)
			}
			branch.Label = strings.Join(labels, " ")
		}
		if board, e := game.ValidateFEN(b.FEN); e == nil {
			branch.Turn = "White"
			if board.SideToMove == chess.Black {
				branch.Turn = "Black"
			}
			if check, _ := board.IsCheckOrMate(); check {
				for square, piece := range board.Piece {
					if piece.Type() == chess.King && piece.Color() == board.SideToMove {
						branch.CheckSquare = chess.Sq(square).String()
						break
					}
				}
			}
			if !b.Done {
				for _, m := range board.LegalMoves() {
					branch.Legal = append(branch.Legal, m.Uci(board))
				}
			}
		}
		v.Branches = append(v.Branches, branch)
	}
	p.Puzzle = v
	return p, nil
}
func (s *Server) puzzleDetail(w http.ResponseWriter, r *http.Request) {
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil {
		s.fail(w, r, store.ErrDenied)
		return
	}
	d, err := s.store.Definition(r.Context(), r.PathValue("collection"), slot)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.puzzleData(r, d, puzzle.InitialState(d))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, "puzzle-detail", p, 200)
}
func (s *Server) puzzleMove(w http.ResponseWriter, r *http.Request) {
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil {
		s.fail(w, r, store.ErrDenied)
		return
	}
	var in store.PuzzleMove
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&in); err != nil {
		s.fail(w, r, store.ErrInvalid)
		return
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		s.fail(w, r, store.ErrInvalid)
		return
	}
	p := s.page(r, "Puzzle")
	d, state, err := s.store.PlayPuzzle(r.Context(), p.User.ID, r.PathValue("collection"), slot, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, err = s.puzzleData(r, d, state)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, "puzzle-state", p, 200)
}
