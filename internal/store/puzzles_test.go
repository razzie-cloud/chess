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
		if d.Slot == 0 || d.Slot == 9 || d.Slot == 35 {
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

func startPuzzle(t *testing.T, s *Store, owner, collection string, slot int) Attempt {
	t.Helper()
	a, err := s.StartAttempt(t.Context(), owner, collection, slot, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func prescribed(t *testing.T, s *Store, a Attempt, branch PuzzleBranch) PuzzleMove {
	t.Helper()
	d, err := s.Definition(t.Context(), a.CollectionID, a.Slot)
	if err != nil {
		t.Fatal(err)
	}
	n, err := puzzle.Lookup(d, branch.Path)
	if err != nil {
		t.Fatal(err)
	}
	return PuzzleMove{Branch: branch.ID, ContentDigest: a.Digest, Path: append([]string{}, branch.Path...), UCI: n.Move, Revision: a.Revision}
}

func stepPuzzle(t *testing.T, s *Store, a Attempt, branch PuzzleBranch) Attempt {
	t.Helper()
	next, err := s.PuzzleMove(t.Context(), a.ID, a.OwnerID, prescribed(t, s, a, branch))
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func solvePuzzle(t *testing.T, s *Store, a Attempt) Attempt {
	t.Helper()
	for range 20 {
		if a.Status == "solved" {
			return a
		}
		found := false
		for _, b := range a.Branches {
			if b.Status == "pending" {
				a = stepPuzzle(t, s, a, b)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("attempt has no pending branches: %+v", a)
		}
	}
	t.Fatal("puzzle did not terminate")
	return a
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
	solvePuzzle(t, s, startPuzzle(t, s, owner, collection, 0))
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

func TestPuzzleForkRequiresEveryLineAndReplaysRetiredTransitions(t *testing.T) {
	s := isolatedStore(t, 1)
	collection := seedFixture(t, s)
	owner, stranger := uuid.NewString(), uuid.NewString()
	a := startPuzzle(t, s, owner, collection, 9)
	_, err := s.Attempt(t.Context(), a.ID, stranger)
	requireDenied(t, err)
	first := prescribed(t, s, a, a.Branches[0])
	wrong := first
	wrong.UCI = "a1a2"
	if _, err := s.PuzzleMove(t.Context(), a.ID, owner, wrong); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-prescribed move accepted: %v", err)
	}
	if next, err := s.Attempt(t.Context(), a.ID, owner); err != nil || next.Revision != 0 {
		t.Fatal("incorrect move changed state")
	}
	forged := first
	forged.Path = []string{first.UCI, "a1a2"}
	if _, err := s.PuzzleMove(t.Context(), a.ID, owner, forged); err == nil {
		t.Fatal("node jump accepted")
	}
	if _, err := s.PuzzleMove(t.Context(), a.ID, stranger, first); !errors.Is(err, ErrDenied) {
		t.Fatal("cross-user move accepted")
	}
	a, err = s.PuzzleMove(t.Context(), a.ID, owner, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Branches) != 3 || a.Branches[0].Status != "expanded" {
		t.Fatalf("missing two child boards: %+v", a.Branches)
	}
	stale := prescribed(t, s, a, a.Branches[2])
	a = stepPuzzle(t, s, a, a.Branches[1])
	if a.Status != "active" {
		t.Fatal("first completed line solved the entire puzzle")
	}
	cards, err := s.Collections(t.Context(), owner)
	if err != nil || cards[0].Solved != 0 {
		t.Fatal("partial completion awarded bit")
	}
	if _, err := s.PuzzleMove(t.Context(), a.ID, owner, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale other-tab revision accepted")
	}
	a = solvePuzzle(t, s, a)
	for _, b := range a.Branches {
		if b.Status != "solved" {
			t.Fatal("parent did not become solved after all children")
		}
	}
	replay, err := s.PuzzleMove(t.Context(), a.ID, owner, first)
	if err != nil || replay.Status != "solved" || replay.Revision != a.Revision {
		t.Fatalf("retired source retry failed: %v", err)
	}
	first.UCI = "a1a2"
	if _, err := s.PuzzleMove(t.Context(), a.ID, owner, first); !errors.Is(err, ErrPayload) {
		t.Fatal("changed accepted transition payload accepted")
	}
	cards, err = s.Collections(t.Context(), owner)
	if err != nil || cards[0].Solved != 1 {
		t.Fatal("all-lines solve failed to set bit")
	}
	for _, slot := range cards[0].Slots {
		if slot.Slot == 9 && !slot.Solved {
			t.Fatal("solved slot not reported")
		}
	}
}

func TestPuzzleParallelBitsSlotsZeroAnd255AndRequestReceipts(t *testing.T) {
	s := isolatedStore(t, 4)
	collection := seedFixture(t, s)
	owner := uuid.NewString()
	var attempts []Attempt
	var final []PuzzleMove
	for _, slot := range []int{0, 255} {
		a := startPuzzle(t, s, owner, collection, slot)
		a = stepPuzzle(t, s, a, a.Branches[0])
		attempts = append(attempts, a)
		final = append(final, prescribed(t, s, a, a.Branches[0]))
	}
	results := make(chan error, 2)
	for i, a := range attempts {
		go func() { _, err := s.PuzzleMove(t.Context(), a.ID, owner, final[i]); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count, first, last int
	if err := s.DB.QueryRow(`SELECT bit_count(solved),get_bit(solved,0),get_bit(solved,255) FROM user_puzzle_progress WHERE user_id=$1 AND collection_id=$2`, owner, collection).Scan(&count, &first, &last); err != nil || count != 2 || first != 1 || last != 1 {
		t.Fatalf("parallel progress lost bits: %d %d %d %v", count, first, last, err)
	}
	request := uuid.NewString()
	a, err := s.StartAttempt(t.Context(), owner, collection, 0, request, true)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.StartAttempt(t.Context(), owner, collection, 0, request, true)
	if err != nil || replay.ID != a.ID {
		t.Fatal("start receipt did not replay")
	}
	if _, err := s.StartAttempt(t.Context(), owner, collection, 255, request, true); !errors.Is(err, ErrPayload) {
		t.Fatal("start receipt accepted changed payload")
	}
	end := uuid.NewString()
	ended, err := s.EndAttempt(t.Context(), a.ID, owner, end, a.Revision)
	if err != nil || ended.Status != "ended" {
		t.Fatal("end failed")
	}
	replay, err = s.EndAttempt(t.Context(), a.ID, owner, end, a.Revision)
	if err != nil || replay.Revision != ended.Revision {
		t.Fatal("end receipt did not replay")
	}
	if _, err := s.EndAttempt(t.Context(), a.ID, owner, end, a.Revision+1); !errors.Is(err, ErrPayload) {
		t.Fatal("end changed payload accepted")
	}
	solvePuzzle(t, s, startPuzzle(t, s, owner, collection, 0))
	if err := s.DB.QueryRow(`SELECT bit_count(solved) FROM user_puzzle_progress WHERE user_id=$1 AND collection_id=$2`, owner, collection).Scan(&count); err != nil || count != 2 {
		t.Fatal("repeat solve/end cleared or duplicated bits")
	}
}

func TestPuzzleThreeReplyForkRestartAndOneConnectionPool(t *testing.T) {
	s := isolatedStore(t, 1)
	collection := seedFixture(t, s)
	owner := uuid.NewString()
	a := startPuzzle(t, s, owner, collection, 35)
	a = stepPuzzle(t, s, a, a.Branches[0])
	if len(a.Branches) != 4 {
		t.Fatal("three defenses not materialized")
	}
	resumed := startPuzzle(t, s, owner, collection, 35)
	if resumed.ID != a.ID || resumed.Revision != a.Revision {
		t.Fatal("partial attempt did not resume")
	}
	restarted, err := s.StartAttempt(t.Context(), owner, collection, 35, uuid.NewString(), true)
	if err != nil || restarted.ID == a.ID {
		t.Fatal("restart did not make fresh attempt")
	}
	old, err := s.Attempt(t.Context(), a.ID, owner)
	if err != nil || old.Status != "ended" {
		t.Fatal("restart left old attempt live")
	}
	a = solvePuzzle(t, s, restarted)
	last := New(s.DB)
	after, err := last.Attempt(t.Context(), a.ID, owner)
	if err != nil || after.Status != "solved" || after.Revision != a.Revision {
		t.Fatal("replica restart lost attempt")
	}
}
