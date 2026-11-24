package db

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func testDatabase(t *testing.T) (*sql.DB, string) {
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
	schema := "chess_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	return database, u.String()
}

func TestConcurrentMigrationsAndRestart(t *testing.T) {
	database, uri := testDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	const replicas = 4
	start := make(chan struct{})
	results := make(chan error, replicas)
	for range replicas {
		pool, err := sql.Open("pgx", uri)
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(2)
		t.Cleanup(func() { pool.Close() })
		go func() {
			<-start
			results <- Migrate(ctx, pool)
		}()
	}
	close(start)
	for range replicas {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version WHERE is_applied AND version_id > 0").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration count = %d: %v", count, err)
	}
	id, owner := uuid.NewString(), uuid.NewString()
	insert := `INSERT INTO matches(id,creator_id,creator_color,guest_name,mode,initial_fen,current_fen) VALUES($1,$2,'w','Guest','standard','fixture','fixture')`
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, insert, id, owner); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM matches WHERE id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback left match: %d %v", count, err)
	}
	if _, err = database.ExecContext(ctx, insert, id, owner); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM matches WHERE id=$1 AND creator_id=$2`, id, owner).Scan(&count); err != nil || count != 1 {
		t.Fatalf("restart lost match: %d %v", count, err)
	}
}

func TestSlotBoundsAndAtomicProgress(t *testing.T) {
	database, _ := testDatabase(t)
	ctx := t.Context()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO puzzle_collections(id,title) VALUES('fixture-v1','Fixture')`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO puzzles(collection_id,slot,initial_fen,solver_color,mate_limit,solution,content_digest,fork_count,line_count) VALUES('fixture-v1',$1,'fixture','w',1,'{}','fixture',0,1)`
	for _, slot := range []int{0, 255} {
		if _, err := database.ExecContext(ctx, insert, slot); err != nil {
			t.Fatal(err)
		}
	}
	for _, slot := range []int{-1, 256} {
		if _, err := database.ExecContext(ctx, insert, slot); err == nil {
			t.Fatalf("invalid slot %d accepted", slot)
		}
	}
	owner := uuid.NewString()
	results := make(chan error, 2)
	for _, slot := range []int{0, 255} {
		go func() {
			_, err := database.ExecContext(ctx, `INSERT INTO user_puzzle_progress(user_id,collection_id,solved) VALUES($1,'fixture-v1',set_bit(B'0'::bit(256),$2,1)) ON CONFLICT(user_id,collection_id) DO UPDATE SET solved=set_bit(user_puzzle_progress.solved,$2,1)`, owner, slot)
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count, first, last int
	if err := database.QueryRowContext(ctx, `SELECT bit_count(solved),get_bit(solved,0),get_bit(solved,255) FROM user_puzzle_progress WHERE user_id=$1`, owner).Scan(&count, &first, &last); err != nil || count != 2 || first != 1 || last != 1 {
		t.Fatalf("progress lost: %d %d %d %v", count, first, last, err)
	}
}
