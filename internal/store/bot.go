package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
)

func replayMatch(m Snapshot) (*game.Game, error) {
	moves := make([]string, len(m.Moves))
	for i, mv := range m.Moves {
		moves[i] = mv.UCI
	}
	return game.Replay(m.InitialFEN, moves, m.Mode == "chess960")
}

// Called while holding the match lock, in the same transaction as its mutation.
func syncBotJob(ctx context.Context, tx *sql.Tx, m Snapshot) error {
	if m.BotLevel == 0 {
		return nil
	}
	g, err := replayMatch(m)
	if err != nil {
		return err
	}
	if m.Status != "active" || (g.Board.SideToMove == 0) == (m.CreatorColor == "w") {
		_, err = tx.ExecContext(ctx, `DELETE FROM match_bot_jobs WHERE match_id=$1`, m.ID)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO match_bot_jobs(match_id,id,revision,pgn,level) VALUES($1,$2,$3,$4,$5)
        ON CONFLICT(match_id) DO UPDATE SET id=EXCLUDED.id,revision=EXCLUDED.revision,pgn=EXCLUDED.pgn,level=EXCLUDED.level,retry_at=now()
        WHERE match_bot_jobs.revision<>EXCLUDED.revision`, m.ID, uuid.NewString(), m.Revision, game.ExportPGN(g), m.BotLevel)
	return err
}

type BotJob struct {
	ID, PGN string
	Level   int
}

// A short lease coordinates dispatchers across Chess replicas. No network call
// holds database locks, and a lost submission/callback becomes eligible again.
func (s *Store) ClaimBotJob(ctx context.Context) (BotJob, error) {
	var out BotJob
	err := s.DB.QueryRowContext(ctx, `UPDATE match_bot_jobs SET retry_at=now()+interval '30 seconds'
        WHERE match_id=(SELECT match_id FROM match_bot_jobs WHERE retry_at<=now() ORDER BY retry_at LIMIT 1 FOR UPDATE SKIP LOCKED)
        RETURNING id,pgn,level`).Scan(&out.ID, &out.PGN, &out.Level)
	return out, err
}

func (s *Store) RetryBotJob(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE match_bot_jobs SET retry_at=now()+interval '5 seconds' WHERE id=$1`, id)
	return err
}

// ApplyBotMove authenticates the seat through the durable job, then rechecks
// revision, turn and legality under the same match lock used by human moves.
// Obsolete/duplicate/deleted job callbacks are acknowledged without mutations.
func (s *Store) ApplyBotMove(ctx context.Context, jobID, uci string) error {
	if !validID(jobID) {
		return ErrInvalid
	}
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT match_id FROM match_bot_jobs WHERE id=$1`, jobID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	m, err := scanMatch(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM matches WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM match_bot_jobs WHERE id=$1 AND match_id=$2 FOR UPDATE`, jobID, id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.BotLevel == 0 || m.Status != "active" || m.Revision != revision {
		return ErrConflict
	}
	// The owner authorization here only loads the history; the seat check below
	// explicitly requires the opposite color before applying any engine move.
	snap, _, err := s.matchTx(ctx, tx, id, Auth{Owner: m.CreatorID}, true)
	if err != nil {
		return err
	}
	g, err := replayMatch(snap)
	if err != nil {
		return err
	}
	if (g.Board.SideToMove == 0) == (m.CreatorColor == "w") {
		return ErrDenied
	}
	mv, err := g.Apply(uci)
	if err != nil {
		return fmt.Errorf("%w: illegal bot move", ErrInvalid)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO match_moves(match_id,ply,uci,san,fen,actor) VALUES($1,$2,$3,$4,$5,'bot')`, id, len(snap.Moves)+1, mv.UCI, mv.SAN, mv.FEN); err != nil {
		return err
	}
	status := "active"
	var completed any
	if g.Result != "*" {
		status = "completed"
		completed = time.Now().UTC()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE matches SET current_fen=$2,result=$3,reason=$4,status=$5,completed_at=$6,revision=revision+1,updated_at=now() WHERE id=$1`, id, g.Board.Fen(), g.Result, g.Reason, status, completed); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM match_bot_jobs WHERE match_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
