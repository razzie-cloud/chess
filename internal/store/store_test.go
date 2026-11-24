package store

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/razzie-cloud/chess/internal/db"
)

func isolatedStore(t *testing.T, maxConns int) *Store {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL unset; disposable PostgreSQL integration test")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("TEST_DATABASE_URL must use a disposable loopback PostgreSQL server")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := "chess_store_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	database, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(maxConns)
	return New(database)
}

func createMatch(t *testing.T, s *Store, owner, color string) Created {
	t.Helper()
	out, err := s.Create(t.Context(), owner, uuid.NewString(), Setup{Name: "Guest opponent", Color: color, Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidToken(out.PlayingToken) || !ValidToken(out.ViewToken) || out.PlayingToken == out.ViewToken {
		t.Fatal("missing independent random capabilities")
	}
	return out
}

func requireSnapshot(t *testing.T, s *Store, id string, auth Auth) Snapshot {
	t.Helper()
	out, err := s.Snapshot(t.Context(), id, auth)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func requireDenied(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
}

func TestMatchScopesAndMoveReceipts(t *testing.T) {
	s := isolatedStore(t, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	owner, stranger := uuid.NewString(), uuid.NewString()
	created := createMatch(t, s, owner, "w")
	ownerAuth := Auth{Owner: owner}
	pending := requireSnapshot(t, s, created.ID, ownerAuth)
	if pending.Status != "pending" || pending.Color != "w" || pending.Role != "owner" {
		t.Fatalf("bad creator state: %+v", pending)
	}
	_, err := s.Snapshot(ctx, created.ID, Auth{Owner: stranger})
	requireDenied(t, err)
	_, err = s.Snapshot(ctx, created.ID, Auth{})
	requireDenied(t, err)
	_, err = s.Owned(ctx, created.ID, stranger)
	if err == nil {
		t.Fatal("go-rel owner lookup exposed stranger match")
	}
	if _, err = s.Owned(ctx, created.ID, owner); err != nil {
		t.Fatalf("go-rel owner lookup: %v", err)
	}
	id, session, err := s.Exchange(ctx, created.PlayingToken)
	if err != nil || id != created.ID || !ValidToken(session) {
		t.Fatalf("exchange: %s %v", id, err)
	}
	guestAuth := Auth{Session: session}
	active := requireSnapshot(t, s, id, guestAuth)
	if active.Status != "active" || active.Revision != pending.Revision+1 || active.Color != "b" {
		t.Fatalf("activation: %+v", active)
	}
	_, anotherSession, err := s.Exchange(ctx, created.PlayingToken)
	if err != nil || anotherSession == session {
		t.Fatalf("reusable guest link: %v", err)
	}
	if got := requireSnapshot(t, s, id, Auth{Session: anotherSession}); got.Revision != active.Revision {
		t.Fatal("repeated exchange reactivated match")
	}
	move := Mutation{RequestID: uuid.NewString(), Revision: active.Revision, Action: "move", Value: "e2e4"}
	_, _, err = s.Mutate(ctx, id, guestAuth, move)
	requireDenied(t, err)
	viewer := Auth{View: created.ViewToken}
	_, _, err = s.Mutate(ctx, id, viewer, move)
	requireDenied(t, err)
	after, _, err := s.Mutate(ctx, id, ownerAuth, move)
	if err != nil || len(after.Moves) != 1 || after.Revision != active.Revision+1 {
		t.Fatalf("owner move: %+v %v", after, err)
	}
	replayed, _, err := s.Mutate(ctx, id, ownerAuth, move)
	if err != nil || replayed.Revision != after.Revision || len(replayed.Moves) != 1 {
		t.Fatalf("duplicate move: %+v %v", replayed, err)
	}
	changed := move
	changed.Value = "d2d4"
	_, _, err = s.Mutate(ctx, id, ownerAuth, changed)
	if !errors.Is(err, ErrPayload) {
		t.Fatalf("changed replay payload: %v", err)
	}
	stale := move
	stale.RequestID = uuid.NewString()
	_, _, err = s.Mutate(ctx, id, ownerAuth, stale)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale move: %v", err)
	}
	blackMove := Mutation{RequestID: uuid.NewString(), Revision: after.Revision, Action: "move", Value: "e7e5"}
	black, _, err := s.Mutate(ctx, id, guestAuth, blackMove)
	if err != nil || len(black.Moves) != 2 {
		t.Fatalf("guest move: %+v %v", black, err)
	}
	viewed := requireSnapshot(t, s, id, viewer)
	if viewed.CurrentFEN != black.CurrentFEN || len(viewed.Moves) != 2 || viewed.Role != "viewer" || viewed.Color != "" {
		t.Fatal("viewer snapshot did not match authorized history")
	}
	if err := s.Presence(ctx, id, guestAuth, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if got := requireSnapshot(t, s, id, ownerAuth); got.Revision != black.Revision || got.Viewers != 1 {
		t.Fatal("presence advanced game revision or omitted lease")
	}
}

func TestGrantRotationAndPerMatchSessions(t *testing.T) {
	s := isolatedStore(t, 1)
	ctx := t.Context()
	owner := uuid.NewString()
	first, second := createMatch(t, s, owner, "w"), createMatch(t, s, owner, "b")
	_, session1, err := s.Exchange(ctx, first.PlayingToken)
	if err != nil {
		t.Fatal(err)
	}
	_, session2, err := s.Exchange(ctx, second.PlayingToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Snapshot(ctx, first.ID, Auth{Session: session2})
	requireDenied(t, err)
	_, err = s.Snapshot(ctx, second.ID, Auth{Session: session1})
	requireDenied(t, err)
	state := requireSnapshot(t, s, first.ID, Auth{Owner: owner})
	replace := Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: "guest-replace"}
	_, replacement, err := s.Mutate(ctx, first.ID, Auth{Owner: owner}, replace)
	if err != nil || !ValidToken(replacement) || replacement == first.PlayingToken {
		t.Fatalf("playing rotation: %v", err)
	}
	_, err = s.Snapshot(ctx, first.ID, Auth{Session: session1})
	requireDenied(t, err)
	_, _, err = s.Exchange(ctx, first.PlayingToken)
	requireDenied(t, err)
	requireSnapshot(t, s, second.ID, Auth{Session: session2})
	requireSnapshot(t, s, first.ID, Auth{View: first.ViewToken})
	_, secretReplay, err := s.Mutate(ctx, first.ID, Auth{Owner: owner}, replace)
	if err != nil || secretReplay != "" {
		t.Fatalf("playing secret replay exposed/recreated token: %v", err)
	}
	_, freshSession, err := s.Exchange(ctx, replacement)
	if err != nil {
		t.Fatal(err)
	}
	state = requireSnapshot(t, s, first.ID, Auth{Owner: owner})
	_, _, err = s.Mutate(ctx, first.ID, Auth{Owner: owner}, Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: "view-revoke"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Snapshot(ctx, first.ID, Auth{View: first.ViewToken})
	requireDenied(t, err)
	state = requireSnapshot(t, s, first.ID, Auth{Session: freshSession})
	if state.ViewToken != "" {
		t.Fatal("revoked viewing link remained exposed to guest")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE guest_sessions SET expires_at=now()-interval '1 second' WHERE digest=$1`, Digest(freshSession)); err != nil {
		t.Fatal(err)
	}
	_, err = s.Snapshot(ctx, first.ID, Auth{Session: freshSession})
	requireDenied(t, err)
	requireSnapshot(t, s, first.ID, Auth{Owner: owner})
}

func TestArchiveDeletionAndCreationTombstone(t *testing.T) {
	s := isolatedStore(t, 1)
	ctx := t.Context()
	owner, request := uuid.NewString(), uuid.NewString()
	setup := Setup{Name: "Opponent", Color: "w", Mode: "standard"}
	created, err := s.Create(ctx, owner, request, setup)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Create(ctx, owner, request, setup)
	if err != nil || retry.ID != created.ID || !retry.Replay || retry.PlayingToken != "" {
		t.Fatalf("creation replay: %+v %v", retry, err)
	}
	changed := setup
	changed.Color = "b"
	_, err = s.Create(ctx, owner, request, changed)
	if !errors.Is(err, ErrPayload) {
		t.Fatalf("changed setup accepted: %v", err)
	}
	_, session, err := s.Exchange(ctx, created.PlayingToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE matches SET created_at=now()-interval '20 years',updated_at=now()-interval '20 years' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	state := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
	if state.Status != "active" {
		t.Fatal("old match expired")
	}
	ended, _, err := s.Mutate(ctx, created.ID, Auth{Session: session}, Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: "resign"})
	if err != nil || ended.Status != "completed" || ended.Result != "1-0" || ended.CompletedAt == nil {
		t.Fatalf("own-color resignation/archive: %+v %v", ended, err)
	}
	active, err := s.List(ctx, owner, Filter{})
	if err != nil || len(active) != 0 {
		t.Fatalf("completed match left in active list: %v", err)
	}
	archive, err := s.List(ctx, owner, Filter{Archive: true, Search: "Opponent"})
	if err != nil || len(archive) != 1 || archive[0].ID != created.ID {
		t.Fatalf("archive missing match: %+v %v", archive, err)
	}
	others, err := s.List(ctx, uuid.NewString(), Filter{Archive: true})
	if err != nil || len(others) != 0 {
		t.Fatal("archive leaked another owner's match")
	}
	deletion := Mutation{RequestID: uuid.NewString(), Revision: ended.Revision, Action: "delete", Value: "confirmed"}
	_, _, err = s.Mutate(ctx, created.ID, Auth{Owner: owner}, deletion)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Mutate(ctx, created.ID, Auth{Owner: owner}, deletion)
	if err != nil {
		t.Fatalf("delete retry: %v", err)
	}
	for _, auth := range []Auth{{Owner: owner}, {Session: session}, {View: created.ViewToken}} {
		_, err = s.Snapshot(ctx, created.ID, auth)
		requireDenied(t, err)
	}
	retry, err = s.Create(ctx, owner, request, setup)
	if err != nil || !retry.Deleted || retry.ID != created.ID {
		t.Fatalf("creation retry recreated deleted match: %+v %v", retry, err)
	}
	for _, table := range []string{"matches", "match_moves", "match_guest_links", "match_view_links", "guest_sessions", "match_viewers"} {
		var count int
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("delete left %s rows: %d %v", table, count, err)
		}
	}
}

func TestParallelMovesHaveOneWinner(t *testing.T) {
	s := isolatedStore(t, 4)
	owner := uuid.NewString()
	created := createMatch(t, s, owner, "w")
	if _, _, err := s.Exchange(t.Context(), created.PlayingToken); err != nil {
		t.Fatal(err)
	}
	state := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, move := range []string{"e2e4", "d2d4"} {
		go func() {
			ready.Done()
			<-start
			_, _, err := s.Mutate(t.Context(), created.ID, Auth{Owner: owner}, Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: "move", Value: move})
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	winners, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			winners++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("competing moves: %d winners, %d conflicts", winners, conflicts)
	}
	if got := requireSnapshot(t, s, created.ID, Auth{Owner: owner}); len(got.Moves) != 1 || got.Revision != state.Revision+1 {
		t.Fatal("parallel moves committed duplicate history")
	}
}

func TestConcurrentCreationUsesOneReceipt(t *testing.T) {
	s := isolatedStore(t, 4)
	owner, request := uuid.NewString(), uuid.NewString()
	setup := Setup{Name: "Guest", Color: "w", Mode: "standard"}
	type result struct {
		created Created
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			created, err := s.Create(t.Context(), owner, request, setup)
			results <- result{created, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.created.ID != b.created.ID || a.created.Replay == b.created.Replay {
		t.Fatalf("creation race: %+v %+v", a, b)
	}
	if (a.created.PlayingToken == "") == (b.created.PlayingToken == "") {
		t.Fatal("only the original creation may return its playing credential")
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM matches`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent creation inserted %d matches: %v", count, err)
	}
}

func TestGuestMoveRacesWithRevocationAndDeletion(t *testing.T) {
	for _, action := range []string{"guest-revoke", "delete"} {
		t.Run(action, func(t *testing.T) {
			s := isolatedStore(t, 4)
			owner := uuid.NewString()
			created := createMatch(t, s, owner, "b")
			_, session, err := s.Exchange(t.Context(), created.PlayingToken)
			if err != nil {
				t.Fatal(err)
			}
			state := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
			guestMove := Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: "move", Value: "e2e4"}
			management := Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: action}
			if action == "delete" {
				management.Value = "confirmed"
			}
			start := make(chan struct{})
			guestResult, ownerResult := make(chan error, 1), make(chan error, 1)
			go func() {
				<-start
				_, _, err := s.Mutate(t.Context(), created.ID, Auth{Session: session}, guestMove)
				guestResult <- err
			}()
			go func() {
				<-start
				_, _, err := s.Mutate(t.Context(), created.ID, Auth{Owner: owner}, management)
				ownerResult <- err
			}()
			close(start)
			guestErr, ownerErr := <-guestResult, <-ownerResult
			switch {
			case guestErr == nil && errors.Is(ownerErr, ErrConflict):
				latest := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
				if len(latest.Moves) != 1 {
					t.Fatal("winning guest move lost its history")
				}
				management.RequestID = uuid.NewString()
				management.Revision = latest.Revision
				if _, _, err := s.Mutate(t.Context(), created.ID, Auth{Owner: owner}, management); err != nil {
					t.Fatal(err)
				}
			case ownerErr == nil && errors.Is(guestErr, ErrDenied):
			default:
				t.Fatalf("unordered race outcomes: owner=%v guest=%v", ownerErr, guestErr)
			}
			_, _, err = s.Mutate(t.Context(), created.ID, Auth{Session: session}, guestMove)
			requireDenied(t, err)
			if action == "delete" {
				_, err = s.Snapshot(t.Context(), created.ID, Auth{Owner: owner})
				requireDenied(t, err)
			} else {
				if got := requireSnapshot(t, s, created.ID, Auth{Owner: owner}); got.Status != "active" {
					t.Fatal("revocation changed match lifecycle")
				}
			}
		})
	}
}

func TestSessionPaginationIsStableAndOwnerScoped(t *testing.T) {
	s := isolatedStore(t, 1)
	owner := uuid.NewString()
	createMatch(t, s, uuid.NewString(), "w")
	for range 3 {
		createMatch(t, s, owner, "w")
	}
	var ids []string
	before := ""
	for range 4 {
		page, err := s.List(t.Context(), owner, Filter{Before: before, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) != 1 || page[0].CreatorID != owner {
			t.Fatal("pagination escaped owner or limit")
		}
		ids = append(ids, page[0].ID)
		before = page[0].CreatedAt.Format(time.RFC3339Nano) + "|" + page[0].ID
	}
	if len(ids) != 3 || ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] {
		t.Fatalf("pagination skipped or repeated matches: %v", ids)
	}
	page, err := s.List(t.Context(), owner, Filter{Search: "%"})
	if err != nil || len(page) != 0 {
		t.Fatalf("search wildcard was not treated literally: %v", err)
	}
}

func TestPGNImportPersistsVariantForReconstruction(t *testing.T) {
	s := isolatedStore(t, 1)
	owner := uuid.NewString()
	created, err := s.Create(t.Context(), owner, uuid.NewString(), Setup{Name: "Guest", Color: "w", Mode: "custom", PGN: "[Variant \"Chess960\"]\n[SetUp \"1\"]\n[FEN \"4k3/8/8/8/8/8/8/RK5R w HA - 0 1\"]\n\n*"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Exchange(t.Context(), created.PlayingToken)
	if err != nil {
		t.Fatal(err)
	}
	state := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
	if state.Mode != "chess960" {
		t.Fatal("import discarded Chess960 castling semantics")
	}
	castled, _, err := s.Mutate(t.Context(), created.ID, Auth{Owner: owner}, Mutation{RequestID: uuid.NewString(), Revision: state.Revision, Action: "move", Value: "b1a1"})
	if err != nil || len(castled.Moves) != 1 {
		t.Fatalf("imported Chess960 castle failed: %v", err)
	}
}
