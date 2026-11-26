package puzzle

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/razzie-cloud/chess/internal/game"
)

// PlayState travels with every move. Boards are verified by replaying every
// decision from the immutable definition, including all mandatory defenses.
type PlayState struct {
	Moves  []Decision `json:"moves"`
	Boards []Board    `json:"boards"`
}
type Decision struct {
	Branch string `json:"branch"`
	UCI    string `json:"uci"`
}
type Board struct {
	ID        string   `json:"id"`
	Path      []string `json:"path"`
	FEN       string   `json:"fen"`
	Remaining int      `json:"remaining"`
	Done      bool     `json:"done"`
}

func InitialState(d Definition) PlayState {
	return PlayState{Moves: []Decision{}, Boards: []Board{{ID: "root", Path: []string{}, FEN: d.FEN, Remaining: d.Mate}}}
}

func (s PlayState) Done() bool {
	if len(s.Boards) == 0 {
		return false
	}
	for _, b := range s.Boards {
		if !b.Done {
			return false
		}
	}
	return true
}

// VerifyState never accepts a client FEN or completion claim without replay.
func VerifyState(d Definition, submitted PlayState) (PlayState, error) {
	if len(submitted.Moves) > 3*d.Mate || len(submitted.Boards) < 1 || len(submitted.Boards) > 3 {
		return PlayState{}, errors.New("invalid puzzle state")
	}
	state := InitialState(d)
	for _, decision := range submitted.Moves {
		var err error
		state, err = Advance(d, state, decision)
		if err != nil {
			return PlayState{}, errors.New("invalid puzzle history")
		}
	}
	if !reflect.DeepEqual(state.Boards, submitted.Boards) {
		return PlayState{}, errors.New("invalid puzzle boards")
	}
	return state, nil
}

// Advance uses prepared proof nodes; it performs no runtime search.
func Advance(d Definition, state PlayState, decision Decision) (PlayState, error) {
	index := -1
	for i, b := range state.Boards {
		if b.ID == decision.Branch {
			index = i
			break
		}
	}
	if index < 0 || state.Boards[index].Done || len(decision.UCI) > 5 || len(state.Moves) >= 3*d.Mate {
		return PlayState{}, errors.New("invalid puzzle move")
	}
	b := state.Boards[index]
	node, err := Lookup(d, b.Path)
	if err != nil || node.FEN != b.FEN {
		return PlayState{}, errors.New("invalid puzzle source")
	}
	if decision.UCI != node.Move {
		position, err := game.ValidateFEN(node.FEN)
		if err != nil {
			return PlayState{}, err
		}
		move, err := game.LegalMove(position, decision.UCI, false)
		if err != nil || move.Uci(position) != decision.UCI {
			return PlayState{}, errors.New("Not the solution move.")
		}
		after := position.MakeMove(move)
		if check, mate := after.IsCheckOrMate(); !check || !mate {
			return PlayState{}, errors.New("Not the solution move.")
		}
		node = &Node{FEN: node.FEN, Move: decision.UCI, After: after.Fen()}
	}
	played := append(append([]string{}, b.Path...), node.Move)
	remaining := b.Remaining - 1
	var next []Board
	if len(node.Replies) == 0 {
		next = []Board{{ID: b.ID, Path: played, FEN: node.After, Remaining: remaining, Done: true}}
	} else {
		for i, reply := range node.Replies {
			id := b.ID
			if len(node.Replies) > 1 {
				id = fmt.Sprintf("%s-%d", b.ID, i+1)
			}
			path := append(append([]string{}, played...), reply.Move)
			next = append(next, Board{ID: id, Path: path, FEN: reply.FEN, Remaining: remaining})
		}
	}
	boards := append([]Board{}, state.Boards[:index]...)
	boards = append(boards, next...)
	boards = append(boards, state.Boards[index+1:]...)
	if len(boards) > 3 || remaining < 0 {
		return PlayState{}, errors.New("invalid puzzle proof")
	}
	moves := append(append([]Decision{}, state.Moves...), decision)
	return PlayState{Moves: moves, Boards: boards}, nil
}
