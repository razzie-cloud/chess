package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/db"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/puzzle"
)

type PuzzleSlot struct {
	Slot      int
	Title     string
	Solved    bool
	AttemptID string
}
type PuzzleCollection struct {
	ID, Title, Description string
	Total, Solved          int
	Slots                  []PuzzleSlot
}
type PuzzleBranch struct {
	ID, ParentID string
	Path         []string
	FEN          string
	Remaining    int
	Status       string
}
type Attempt struct {
	ID, OwnerID, CollectionID                 string
	Slot                                      int
	Revision                                  int64
	Status, Title, Solver, Digest, InitialFEN string
	Mate                                      int
	Branches                                  []PuzzleBranch
}
type PuzzleMove struct {
	Branch, ContentDigest string
	Path                  []string
	UCI                   string
	Revision              int64
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
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.title,c.description,p.slot,p.title,coalesce(get_bit(u.solved,p.slot),0),coalesce((SELECT a.id::text FROM puzzle_attempts a WHERE a.owner_id=$1 AND a.collection_id=p.collection_id AND a.slot=p.slot AND a.status='active' ORDER BY a.created_at DESC,a.id DESC LIMIT 1),'') FROM puzzle_collections c JOIN puzzles p ON p.collection_id=c.id LEFT JOIN user_puzzle_progress u ON u.collection_id=c.id AND u.user_id=$1 ORDER BY c.published_at,c.id,p.slot`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PuzzleCollection
	for rows.Next() {
		var id, title, description string
		var slot PuzzleSlot
		var solved int
		if err = rows.Scan(&id, &title, &description, &slot.Slot, &slot.Title, &solved, &slot.AttemptID); err != nil {
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

func attemptTx(ctx context.Context, tx *sql.Tx, id, owner string, lock bool) (Attempt, puzzle.Definition, error) {
	var a Attempt
	var d puzzle.Definition
	if !validID(id) || !validID(owner) {
		return a, d, ErrDenied
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	err := tx.QueryRowContext(ctx, `SELECT id,owner_id,collection_id,slot,revision,status FROM puzzle_attempts WHERE id=$1 AND owner_id=$2`+suffix, id, owner).Scan(&a.ID, &a.OwnerID, &a.CollectionID, &a.Slot, &a.Revision, &a.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return a, d, ErrDenied
	}
	if err != nil {
		return a, d, err
	}
	d, err = definition(ctx, tx, a.CollectionID, a.Slot)
	if err != nil {
		return a, d, err
	}
	a.Title = d.Title
	a.Solver = d.Solver
	a.Digest = d.Digest
	a.InitialFEN = d.FEN
	a.Mate = d.Mate
	rows, err := tx.QueryContext(ctx, `SELECT id,coalesce(parent_id,''),path,current_fen,moves_remaining,status FROM puzzle_branches WHERE attempt_id=$1 ORDER BY id`+suffix, id)
	if err != nil {
		return a, d, err
	}
	defer rows.Close()
	for rows.Next() {
		var b PuzzleBranch
		var path []byte
		if err = rows.Scan(&b.ID, &b.ParentID, &path, &b.FEN, &b.Remaining, &b.Status); err != nil {
			return a, d, err
		}
		if err = json.Unmarshal(path, &b.Path); err != nil {
			return a, d, err
		}
		a.Branches = append(a.Branches, b)
	}
	return a, d, rows.Err()
}

func (s *Store) Attempt(ctx context.Context, id, owner string) (Attempt, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	a, _, err := attemptTx(ctx, tx, id, owner, false)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func (s *Store) StartAttempt(ctx context.Context, owner, collection string, slot int, request string, restart bool) (Attempt, error) {
	if !validID(owner) || !validID(request) || slot < 0 || slot > 255 {
		return Attempt{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, owner+":puzzle-start:"+request); err != nil {
		return Attempt{}, err
	}
	digest := canonical(struct {
		Collection string
		Slot       int
		Restart    bool
	}{collection, slot, restart})
	var prior, id string
	err = tx.QueryRowContext(ctx, `SELECT digest,resource_id::text FROM mutation_receipts WHERE actor=$1 AND resource='puzzle-start' AND request_id=$2`, owner, request).Scan(&prior, &id)
	if err == nil {
		if prior != digest {
			return Attempt{}, ErrPayload
		}
		a, _, er := attemptTx(ctx, tx, id, owner, true)
		if er != nil {
			return a, er
		}
		return a, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, err
	}
	d, err := definition(ctx, tx, collection, slot)
	if err != nil {
		return Attempt{}, err
	}
	// Serialize start/resume per owned puzzle without using process-local state.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%s:puzzle:%s:%d", owner, collection, slot)); err != nil {
		return Attempt{}, err
	}
	var ids []string
	rows, err := tx.QueryContext(ctx, `SELECT id FROM puzzle_attempts WHERE owner_id=$1 AND collection_id=$2 AND slot=$3 AND status='active' ORDER BY id FOR UPDATE`, owner, collection, slot)
	if err != nil {
		return Attempt{}, err
	}
	for rows.Next() {
		var existing string
		if err = rows.Scan(&existing); err != nil {
			rows.Close()
			return Attempt{}, err
		}
		ids = append(ids, existing)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Attempt{}, err
	}
	if !restart && len(ids) > 0 {
		id = ids[len(ids)-1]
	} else {
		if restart {
			for _, existing := range ids {
				if _, err = tx.ExecContext(ctx, `UPDATE puzzle_attempts SET status='ended',revision=revision+1,updated_at=now() WHERE id=$1`, existing); err != nil {
					return Attempt{}, err
				}
			}
		}
		id = uuid.NewString()
		if _, err = tx.ExecContext(ctx, `INSERT INTO puzzle_attempts(id,owner_id,collection_id,slot) VALUES($1,$2,$3,$4)`, id, owner, collection, slot); err != nil {
			return Attempt{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO puzzle_branches(attempt_id,id,path,current_fen,node_path,moves_remaining,status) VALUES($1,'root','[]',$2,'',$3,'pending')`, id, d.FEN, d.Mate); err != nil {
			return Attempt{}, err
		}
	}
	if err = receipt(ctx, tx, owner, "puzzle-start", request, digest, id, 0, "started"); err != nil {
		return Attempt{}, err
	}
	a, _, err := attemptTx(ctx, tx, id, owner, false)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func (s *Store) PuzzleMove(ctx context.Context, id, owner string, in PuzzleMove) (Attempt, error) {
	if !validID(owner) || !validID(id) || in.Revision < 0 || len(in.Branch) > 80 || len(in.Path) > 10 || len(in.UCI) > 5 || len(in.ContentDigest) != 64 {
		return Attempt{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	a, d, err := attemptTx(ctx, tx, id, owner, true)
	if err != nil {
		return a, err
	}
	if in.ContentDigest != d.Digest {
		return a, ErrConflict
	}
	key := canonical(struct {
		Branch string
		Path   []string
	}{in.Branch, append([]string{}, in.Path...)})
	payload := canonical(struct{ Content, UCI string }{in.ContentDigest, in.UCI})
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM mutation_receipts WHERE actor=$1 AND resource=$2 AND request_id=$3`, owner, "puzzle:"+id, key).Scan(&prior)
	if err == nil {
		if prior != payload {
			return a, ErrPayload
		}
		return a, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	if a.Status != "active" || a.Revision != in.Revision {
		return a, ErrConflict
	}
	var branch *PuzzleBranch
	for i := range a.Branches {
		if a.Branches[i].ID == in.Branch {
			branch = &a.Branches[i]
			break
		}
	}
	if branch == nil || branch.Status != "pending" || !samePath(branch.Path, in.Path) {
		return a, ErrConflict
	}
	node, err := puzzle.Lookup(d, in.Path)
	if err != nil || node.FEN != branch.FEN {
		return a, ErrConflict
	}
	if node.Move != in.UCI {
		board, err := game.ValidateFEN(node.FEN)
		if err != nil {
			return a, err
		}
		move, err := game.LegalMove(board, in.UCI, false)
		if err != nil || move.Uci(board) != in.UCI {
			return a, fmt.Errorf("%w Not the solution move.", ErrInvalid)
		}
		after := board.MakeMove(move)
		if check, mate := after.IsCheckOrMate(); !check || !mate {
			return a, fmt.Errorf("%w Not the solution move.", ErrInvalid)
		}
		// Immediate mate needs no continuation from the prepared proof.
		node = &puzzle.Node{FEN: node.FEN, Move: in.UCI, After: after.Fen()}
	}
	played := append(append([]string(nil), in.Path...), node.Move)
	remaining := branch.Remaining - 1
	if remaining < 0 {
		return a, errors.New("invalid puzzle remaining budget")
	}
	switch len(node.Replies) {
	case 0:
		encoded, _ := json.Marshal(played)
		_, err = tx.ExecContext(ctx, `UPDATE puzzle_branches SET path=$3,current_fen=$4,node_path=$5,moves_remaining=$6,status='solved' WHERE attempt_id=$1 AND id=$2`, id, branch.ID, string(encoded), node.After, strings.Join(played, " "), remaining)
	case 1:
		r := node.Replies[0]
		path := append(played, r.Move)
		encoded, _ := json.Marshal(path)
		_, err = tx.ExecContext(ctx, `UPDATE puzzle_branches SET path=$3,current_fen=$4,node_path=$5,moves_remaining=$6 WHERE attempt_id=$1 AND id=$2`, id, branch.ID, string(encoded), r.FEN, strings.Join(path, " "), remaining)
	case 2, 3:
		for _, b := range a.Branches {
			if b.Status == "expanded" || b.ParentID != "" {
				return a, errors.New("unexpected second puzzle fork")
			}
		}
		encoded, _ := json.Marshal(played)
		_, err = tx.ExecContext(ctx, `UPDATE puzzle_branches SET path=$3,current_fen=$4,node_path=$5,moves_remaining=$6,status='expanded' WHERE attempt_id=$1 AND id=$2`, id, branch.ID, string(encoded), node.After, strings.Join(played, " "), remaining)
		if err != nil {
			return a, err
		}
		for i, r := range node.Replies {
			path := append(append([]string(nil), played...), r.Move)
			encoded, _ := json.Marshal(path)
			_, err = tx.ExecContext(ctx, `INSERT INTO puzzle_branches(attempt_id,id,parent_id,path,current_fen,node_path,moves_remaining,status) VALUES($1,$2,$3,$4,$5,$6,$7,'pending')`, id, fmt.Sprintf("%s-%d", branch.ID, i+1), branch.ID, string(encoded), r.FEN, strings.Join(path, " "), remaining)
			if err != nil {
				return a, err
			}
		}
	default:
		return a, errors.New("invalid puzzle defense count")
	}
	if err != nil {
		return a, err
	}
	// Only the single expanded parent can exist; all its children must finish.
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM puzzle_branches WHERE attempt_id=$1 AND status='pending'`, id).Scan(&pending); err != nil {
		return a, err
	}
	status := "active"
	if pending == 0 {
		status = "solved"
		if _, err = tx.ExecContext(ctx, `UPDATE puzzle_branches SET status='solved' WHERE attempt_id=$1 AND status='expanded'`, id); err != nil {
			return a, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_puzzle_progress(user_id,collection_id,solved) VALUES($1,$2,set_bit(B'0'::bit(256),$3,1)) ON CONFLICT(user_id,collection_id) DO UPDATE SET solved=set_bit(user_puzzle_progress.solved,$3,1),updated_at=now()`, owner, a.CollectionID, a.Slot); err != nil {
			return a, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE puzzle_attempts SET revision=revision+1,status=$2,updated_at=now() WHERE id=$1`, id, status); err != nil {
		return a, err
	}
	if err = receipt(ctx, tx, owner, "puzzle:"+id, key, payload, id, a.Revision+1, "move"); err != nil {
		return a, err
	}
	a, _, err = attemptTx(ctx, tx, id, owner, false)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Store) EndAttempt(ctx context.Context, id, owner, request string, revision int64) (Attempt, error) {
	if !validID(request) || revision < 0 {
		return Attempt{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	a, _, err := attemptTx(ctx, tx, id, owner, true)
	if err != nil {
		return a, err
	}
	digest := canonical(struct{ Revision int64 }{revision})
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM mutation_receipts WHERE actor=$1 AND resource=$2 AND request_id=$3`, owner, "puzzle-end:"+id, request).Scan(&prior)
	if err == nil {
		if prior != digest {
			return a, ErrPayload
		}
		return a, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	if a.Revision != revision {
		return a, ErrConflict
	}
	if a.Status == "active" {
		if _, err = tx.ExecContext(ctx, `UPDATE puzzle_attempts SET status='ended',revision=revision+1,updated_at=now() WHERE id=$1`, id); err != nil {
			return a, err
		}
		a.Status = "ended"
		a.Revision++
	}
	if err = receipt(ctx, tx, owner, "puzzle-end:"+id, request, digest, id, a.Revision, "ended"); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

// PlayedBranch returns only moves acknowledged on one branch, never the remaining solution.
func (a Attempt) PlayedBranch(id string) (PuzzleBranch, error) {
	for _, b := range a.Branches {
		if b.ID == id {
			return b, nil
		}
	}
	return PuzzleBranch{}, ErrDenied
}
