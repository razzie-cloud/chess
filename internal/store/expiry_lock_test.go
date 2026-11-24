package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGuestExpiryWhileWaitingForMatchLock(t *testing.T) {
	for _, action := range []string{"read", "move", "replay"} {
		t.Run(action, func(t *testing.T) {
			s := isolatedStore(t, 4)
			owner := uuid.NewString()
			created := createMatch(t, s, owner, "b")
			_, session, err := s.Exchange(t.Context(), created.PlayingToken)
			if err != nil {
				t.Fatal(err)
			}
			auth := Auth{Session: session}
			in := Mutation{RequestID: uuid.NewString(), Revision: 1, Action: "move", Value: "e2e4"}
			if action == "replay" {
				if _, _, err := s.Mutate(t.Context(), created.ID, auth, in); err != nil {
					t.Fatal(err)
				}
			}
			before := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			locker, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer locker.Rollback()
			var lockerPID int
			if err = locker.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM matches WHERE id=$1 FOR UPDATE`, created.ID).Scan(&lockerPID); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				if action == "read" {
					_, err := s.Snapshot(ctx, created.ID, auth)
					result <- err
				} else {
					_, _, err := s.Mutate(ctx, created.ID, auth, in)
					result <- err
				}
			}()
			// Observe the guest's transaction waiting before making the session
			// expire; transaction-start time must no longer authorize it afterward.
			for {
				var waiting bool
				if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(pid)))`, lockerPID).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("guest request did not wait for the match lock: %v", err)
				case <-ctx.Done():
					t.Fatal("guest request never queued behind the match lock")
				case <-time.After(5 * time.Millisecond):
				}
			}
			if _, err = s.DB.ExecContext(ctx, `UPDATE guest_sessions SET expires_at=clock_timestamp() WHERE digest=$1`, Digest(session)); err != nil {
				t.Fatal(err)
			}
			if err = locker.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				requireDenied(t, err)
			case <-ctx.Done():
				t.Fatal("guest request did not finish after releasing the match lock")
			}
			after := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
			if after.Revision != before.Revision || len(after.Moves) != len(before.Moves) {
				t.Fatal("expired guest changed match state")
			}
		})
	}
}
