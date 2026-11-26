package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/razzie-cloud/chess/internal/db"
	"github.com/razzie-cloud/chess/internal/puzzle"
)

type PuzzleSlot struct {
	Slot               int
	Title, FEN, Solver string
	Solved             bool
}
type PuzzleCollection struct {
	ID, Title, Description string
	Total, Solved          int
	Slots                  []PuzzleSlot
}
type PuzzleMove struct {
	ContentDigest string           `json:"content_digest"`
	State         puzzle.PlayState `json:"state"`
	puzzle.Decision
}

// Seed validates all bundled proofs before publishing under one replica-wide transaction lock.
func (s *Store) Seed(ctx context.Context) error { return s.SeedBundle(ctx, db.PuzzleSeeds()) }
func (s *Store) SeedBundle(ctx context.Context, data []byte) error {
	if len(data) > 32<<20 {
		return ErrInvalid
	}
	var bundle puzzle.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf("invalid puzzle seed: %w", err)
	}
	if err := puzzle.Verify(bundle); err != nil {
		return fmt.Errorf("invalid puzzle seed: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(734297201)`); err != nil {
		return err
	}
	for _, c := range bundle.Collections {
		if _, err = tx.ExecContext(ctx, `INSERT INTO puzzle_collections(id,title,description) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET title=excluded.title,description=excluded.description`, c.ID, c.Title, c.Description); err != nil {
			return err
		}
		for _, d := range c.Puzzles {
			var existing string
			err = tx.QueryRowContext(ctx, `SELECT content_digest FROM puzzles WHERE collection_id=$1 AND slot=$2`, c.ID, d.Slot).Scan(&existing)
			if err == nil {
				if existing != d.Digest {
					return fmt.Errorf("puzzle edition content conflict at %s/%d", c.ID, d.Slot)
				}
				encoded, _ := json.Marshal(d)
				if _, err = tx.ExecContext(ctx, `UPDATE puzzles SET title=$3,solution=$4 WHERE collection_id=$1 AND slot=$2`, c.ID, d.Slot, d.Title, string(encoded)); err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			encoded, _ := json.Marshal(d)
			if _, err = tx.ExecContext(ctx, `INSERT INTO puzzles(collection_id,slot,initial_fen,solver_color,mate_in,title,solution,content_digest,fork_count,line_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID, d.Slot, d.FEN, d.Solver, d.Mate, d.Title, string(encoded), d.Digest, d.Forks, d.Lines); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) Collections(ctx context.Context, owner string) ([]PuzzleCollection, error) {
	if !validID(owner) {
		return nil, ErrDenied
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.title,c.description,p.slot,p.title,p.initial_fen,p.solver_color,coalesce(get_bit(u.solved,p.slot),0) FROM puzzle_collections c JOIN puzzles p ON p.collection_id=c.id LEFT JOIN user_puzzle_progress u ON u.collection_id=c.id AND u.user_id=$1 ORDER BY c.published_at,c.id,p.slot`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PuzzleCollection
	for rows.Next() {
		var id, title, description string
		var slot PuzzleSlot
		var solved int
		if err = rows.Scan(&id, &title, &description, &slot.Slot, &slot.Title, &slot.FEN, &slot.Solver, &solved); err != nil {
			return nil, err
		}
		slot.Solved = solved == 1
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, PuzzleCollection{ID: id, Title: title, Description: description})
		}
		c := &out[len(out)-1]
		c.Total++
		if slot.Solved {
			c.Solved++
		}
		c.Slots = append(c.Slots, slot)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, tx.Commit()
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func definition(ctx context.Context, q queryer, collection string, slot int) (puzzle.Definition, error) {
	var d puzzle.Definition
	if len(collection) < 1 || len(collection) > 80 || slot < 0 || slot > 255 {
		return d, ErrDenied
	}
	var data []byte
	var digest string
	err := q.QueryRowContext(ctx, `SELECT solution,content_digest FROM puzzles WHERE collection_id=$1 AND slot=$2`, collection, slot).Scan(&data, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrDenied
	}
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(data, &d); err != nil {
		return d, err
	}
	if d.Digest != digest {
		return d, errors.New("published puzzle digest mismatch")
	}
	return d, puzzle.Validate(d)
}
func (s *Store) Definition(ctx context.Context, collection string, slot int) (puzzle.Definition, error) {
	return definition(ctx, s.DB, collection, slot)
}

// PlayPuzzle validates the full client state on every move and persists only
// completed progress. Repeating a request is naturally idempotent.
func (s *Store) PlayPuzzle(ctx context.Context, owner, collection string, slot int, in PuzzleMove) (puzzle.Definition, puzzle.PlayState, error) {
	var state puzzle.PlayState
	if !validID(owner) {
		return puzzle.Definition{}, state, ErrDenied
	}
	d, err := s.Definition(ctx, collection, slot)
	if err != nil {
		return d, state, err
	}
	if in.ContentDigest != d.Digest {
		return d, state, ErrConflict
	}
	state, err = puzzle.VerifyState(d, in.State)
	if err != nil {
		return d, state, fmt.Errorf("%w Invalid puzzle state.", ErrInvalid)
	}
	state, err = puzzle.Advance(d, state, in.Decision)
	if err != nil {
		return d, state, fmt.Errorf("%w %s", ErrInvalid, err)
	}
	if state.Done() {
		_, err = s.DB.ExecContext(ctx, `INSERT INTO user_puzzle_progress(user_id,collection_id,solved) VALUES($1,$2,set_bit(B'0'::bit(256),$3,1)) ON CONFLICT(user_id,collection_id) DO UPDATE SET solved=set_bit(user_puzzle_progress.solved,$3,1),updated_at=now()`, owner, collection, slot)
	}
	return d, state, err
}
