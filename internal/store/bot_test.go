package store

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestBotTurnsPersistAndCallbacksAreIdempotent(t *testing.T) {
	s := isolatedStore(t, 2)
	owner := uuid.NewString()
	for _, color := range []string{"w", "b"} {
		created, err := s.Create(t.Context(), owner, uuid.NewString(), Setup{Opponent: "bot", BotLevel: 3, Color: color, Mode: "standard"})
		if err != nil || created.PlayingToken != "" {
			t.Fatalf("create: %+v %v", created, err)
		}
		a := Auth{Owner: owner}
		m := requireSnapshot(t, s, created.ID, a)
		if m.Status != "active" || m.BotLevel != 3 {
			t.Fatal(m)
		}
		if _, _, err = s.Mutate(t.Context(), m.ID, a, Mutation{uuid.NewString(), m.Revision, "guest-replace", ""}); !errors.Is(err, ErrDenied) {
			t.Fatalf("bot guest grant allowed: %v", err)
		}
		var grants int
		if err = s.DB.QueryRow(`SELECT count(*) FROM match_guest_links WHERE match_id=$1`, m.ID).Scan(&grants); err != nil || grants != 0 {
			t.Fatalf("bot has guest grant: %d %v", grants, err)
		}
		if color == "w" {
			if _, err = s.ClaimBotJob(t.Context()); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("queued before human turn: %v", err)
			}
			if _, _, err = s.Mutate(t.Context(), m.ID, a, Mutation{uuid.NewString(), m.Revision, "move", "e2e4"}); err != nil {
				t.Fatal(err)
			}
		}
		job, err := s.ClaimBotJob(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.ClaimBotJob(t.Context()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("lease ignored: %v", err)
		}
		// A management revision invalidates the old job and queues the same turn again.
		m = requireSnapshot(t, s, m.ID, a)
		if _, _, err = s.Mutate(t.Context(), m.ID, a, Mutation{uuid.NewString(), m.Revision, "view-replace", ""}); err != nil {
			t.Fatal(err)
		}
		if err = s.ApplyBotMove(t.Context(), job.ID, "e2e4"); err != nil {
			t.Fatal(err)
		}
		next, err := s.ClaimBotJob(t.Context())
		if err != nil || next.ID == job.ID {
			t.Fatalf("new revision did not refresh job: %+v %v", next, err)
		}
		move := "e2e4"
		if color == "w" {
			move = "e7e5"
		}
		if err = s.ApplyBotMove(t.Context(), next.ID, "a1a8"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("illegal bot move accepted: %v", err)
		}
		// Independent replicas receive a duplicated callback concurrently.
		other := New(s.DB)
		var wg sync.WaitGroup
		for _, storage := range []*Store{s, other} {
			wg.Go(func() {
				if err := storage.ApplyBotMove(t.Context(), next.ID, move); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		m = requireSnapshot(t, s, m.ID, a)
		plies := 1
		if color == "w" {
			plies = 2
		}
		if len(m.Moves) != plies || m.Moves[plies-1].Actor != "bot" {
			t.Fatalf("callback applied twice or wrong actor: %+v", m.Moves)
		}
		if _, err = s.ClaimBotJob(t.Context()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("bot queued on owner turn: %v", err)
		}
		if _, _, err = s.Mutate(t.Context(), m.ID, a, Mutation{uuid.NewString(), m.Revision, "delete", "confirmed"}); err != nil {
			t.Fatal(err)
		}
		if err = s.ApplyBotMove(t.Context(), next.ID, move); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBotResignationAndDeletionCancelPendingJobs(t *testing.T) {
	s := isolatedStore(t, 2)
	owner := uuid.NewString()
	for _, action := range []string{"resign", "delete"} {
		created, err := s.Create(t.Context(), owner, uuid.NewString(), Setup{Opponent: "bot", BotLevel: 5, Color: "b", Mode: "standard"})
		if err != nil {
			t.Fatal(err)
		}
		job, err := s.ClaimBotJob(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.Mutate(t.Context(), created.ID, Auth{Owner: owner}, Mutation{uuid.NewString(), 0, action, "confirmed"}); err != nil {
			t.Fatal(err)
		}
		if err = s.ApplyBotMove(t.Context(), job.ID, "e2e4"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.ClaimBotJob(t.Context()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("job survived %s: %v", action, err)
		}
	}
}
