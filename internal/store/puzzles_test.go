package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/db"
	"github.com/razzie-cloud/chess/internal/puzzle"
)

func fixtureBundle(t *testing.T) puzzle.Bundle {
	t.Helper()
	var b puzzle.Bundle
	if err := json.Unmarshal(db.PuzzleSeeds(), &b); err != nil {
		t.Fatal(err)
	}
	var selected []puzzle.Definition
	for _, d := range b.Collections[0].Puzzles {
		if d.Slot == 0 || d.Slot == 9 || d.Slot == 34 {
			selected = append(selected, d)
		}
	}
	last := selected[0]
	last.Slot = 255
	last.Digest = puzzle.ContentDigest(last)
	selected = append(selected, last)
	b.Collections[0].Puzzles = selected
	return b
}

func seedFixture(t *testing.T, s *Store) string {
	t.Helper()
	b := fixtureBundle(t)
	data, _ := json.Marshal(b)
	if err := s.SeedBundle(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	return b.Collections[0].ID
}

func solvePuzzle(t *testing.T, s *Store, owner, collection string, slot int) puzzle.PlayState {
	t.Helper()
	d, err := s.Definition(t.Context(), collection, slot)
	if err != nil {
		t.Fatal(err)
	}
	state := puzzle.InitialState(d)
	for !state.Done() {
		var branch puzzle.Board
		for _, b := range state.Boards {
			if !b.Done {
				branch = b
				break
			}
		}
		node, err := puzzle.Lookup(d, branch.Path)
		if err != nil {
			t.Fatal(err)
		}
		_, state, err = s.PlayPuzzle(t.Context(), owner, collection, slot, PuzzleMove{ContentDigest: d.Digest, State: state, Decision: puzzle.Decision{Branch: branch.ID, UCI: node.Move}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return state
}

func TestPuzzleSeedConcurrentImmutableAndEditionProgress(t *testing.T) {
	s := isolatedStore(t, 4)
	bundle := fixtureBundle(t)
	data, _ := json.Marshal(bundle)
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- s.SeedBundle(t.Context(), data) }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM puzzles`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("concurrent seeding duplicated content: %d %v", count, err)
	}
	owner := uuid.NewString()
	collection := bundle.Collections[0].ID
	solvePuzzle(t, s, owner, collection, 0)
	changed := fixtureBundle(t)
	changed.Collections[0].Puzzles[0].Mate++
	changed.Collections[0].Puzzles[0].Digest = puzzle.ContentDigest(changed.Collections[0].Puzzles[0])
	changed.Collections[0].Title = "must roll back"
	bad, _ := json.Marshal(changed)
	if err := s.SeedBundle(t.Context(), bad); err == nil {
		t.Fatal("changed published content overwritten")
	}
	cards, err := s.Collections(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if cards[0].Solved != 1 || cards[0].Title == "must roll back" {
		t.Fatal("seed conflict did not roll back atomically")
	}
	newEdition := fixtureBundle(t)
	newEdition.Collections[0].ID = "next-edition-v1"
	next, _ := json.Marshal(newEdition)
	if err := s.SeedBundle(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Collections(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.ID == "next-edition-v1" && c.Solved != 0 {
			t.Fatal("new edition inherited old completion")
		}
	}
	if _, err := s.Definition(t.Context(), collection, 256); !errors.Is(err, ErrDenied) {
		t.Fatal("slot256 accepted")
	}
}

func TestPuzzleFullStateAllLinesProgressAndReplicaRetries(t *testing.T) {
	s := isolatedStore(t, 1)
	collection := seedFixture(t, s)
	owner := uuid.NewString()
	d, err := s.Definition(t.Context(), collection, 9)
	if err != nil {
		t.Fatal(err)
	}
	first := PuzzleMove{ContentDigest: d.Digest, State: puzzle.InitialState(d), Decision: puzzle.Decision{Branch: "root", UCI: d.Root.Move}}
	_, state, err := s.PlayPuzzle(t.Context(), owner, collection, 9, first)
	if err != nil || len(state.Boards) != 2 {
		t.Fatalf("fork: %+v %v", state, err)
	}
	// Independent replica needs no attempt or receipt state.
	second := New(s.DB)
	_, retry, err := second.PlayPuzzle(t.Context(), owner, collection, 9, first)
	if err != nil || len(retry.Boards) != 2 {
		t.Fatalf("retry: %v", err)
	}
	b := state.Boards[0]
	node, _ := puzzle.Lookup(d, b.Path)
	_, state, err = second.PlayPuzzle(t.Context(), owner, collection, 9, PuzzleMove{ContentDigest: d.Digest, State: state, Decision: puzzle.Decision{Branch: b.ID, UCI: node.Move}})
	if err != nil || !state.Boards[0].Done || state.Done() {
		t.Fatalf("partial: %+v %v", state, err)
	}
	cards, err := s.Collections(t.Context(), owner)
	if err != nil || cards[0].Solved != 0 {
		t.Fatal("partial solve awarded progress")
	}
	b = state.Boards[1]
	node, _ = puzzle.Lookup(d, b.Path)
	final := PuzzleMove{ContentDigest: d.Digest, State: state, Decision: puzzle.Decision{Branch: b.ID, UCI: node.Move}}
	_, state, err = s.PlayPuzzle(t.Context(), owner, collection, 9, final)
	if err != nil || !state.Done() {
		t.Fatalf("completion: %v", err)
	}
	if _, _, err = second.PlayPuzzle(t.Context(), owner, collection, 9, final); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Collections(t.Context(), owner)
	if err != nil || cards[0].Solved != 1 {
		t.Fatal("all lines did not award progress")
	}
	other, err := s.Collections(t.Context(), uuid.NewString())
	if err != nil || other[0].Solved != 0 {
		t.Fatal("progress crossed users")
	}
	var attempts int
	if err = s.DB.QueryRow(`SELECT count(*) FROM puzzle_attempts`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("stateless play created attempts")
	}
}

func TestPuzzleParallelCompletionBitsAndThreeBoards(t *testing.T) {
	s := isolatedStore(t, 4)
	collection := seedFixture(t, s)
	owner := uuid.NewString()
	results := make(chan error, 2)
	for _, slot := range []int{0, 255} {
		d, err := s.Definition(t.Context(), collection, slot)
		if err != nil {
			t.Fatal(err)
		}
		state := puzzle.InitialState(d)
		for {
			b := state.Boards[0]
			node, _ := puzzle.Lookup(d, b.Path)
			next, err := puzzle.Advance(d, state, puzzle.Decision{Branch: b.ID, UCI: node.Move})
			if err != nil {
				t.Fatal(err)
			}
			if next.Done() {
				in := PuzzleMove{ContentDigest: d.Digest, State: state, Decision: puzzle.Decision{Branch: b.ID, UCI: node.Move}}
				go func() { _, _, err := s.PlayPuzzle(t.Context(), owner, collection, slot, in); results <- err }()
				break
			}
			state = next
		}
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count, first, last int
	if err := s.DB.QueryRow(`SELECT bit_count(solved),get_bit(solved,0),get_bit(solved,255) FROM user_puzzle_progress WHERE user_id=$1 AND collection_id=$2`, owner, collection).Scan(&count, &first, &last); err != nil || count != 2 || first != 1 || last != 1 {
		t.Fatalf("lost progress bits: %d %d %d %v", count, first, last, err)
	}
	solvePuzzle(t, s, owner, collection, 0)
	state := solvePuzzle(t, New(s.DB), owner, collection, 34)
	if len(state.Boards) != 3 || !state.Done() {
		t.Fatal("three-line puzzle incomplete")
	}
}
