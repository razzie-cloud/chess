package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-rel/postgres"
	"github.com/go-rel/rel"
	"github.com/go-rel/rel/where"
	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
)

var (
	ErrDenied   = errors.New("This match is no longer available or access has expired.")
	ErrConflict = errors.New("The position changed. Refresh and try again.")
	ErrPayload  = errors.New("This request was already used with different details.")
	ErrInvalid  = errors.New("Invalid request.")
	ErrDeleted  = errors.New("This match was deleted.")
)

type Store struct {
	DB   *sql.DB
	repo rel.Repository
}

func New(db *sql.DB) *Store { return &Store{DB: db, repo: rel.New(postgres.New(db))} }

type Match struct {
	ID, CreatorID, CreatorName, CreatorColor, GuestName, Mode string
	InitialFEN                                                string `db:"initial_fen"`
	CurrentFEN                                                string `db:"current_fen"`
	Revision                                                  int64
	Status, Result, Reason                                    string
	Tags                                                      json.RawMessage
	CreatedAt, UpdatedAt                                      time.Time
	CompletedAt                                               *time.Time
}

func (Match) Table() string { return "matches" }

type Move struct {
	Ply                  int
	UCI, SAN, FEN, Actor string
}
type Snapshot struct {
	Match
	Moves                  []Move
	Role, Color, ViewToken string
	Viewers                int
}
type Auth struct{ Owner, Session, View string }
type Setup struct{ Name, CreatorName, Color, Mode, FEN, PGN string }
type Created struct {
	ID, PlayingToken, ViewToken string
	Replay, Deleted             bool
}
type Mutation struct {
	RequestID     string
	Revision      int64
	Action, Value string
}

func Token() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func Digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func validID(s string) bool  { _, e := uuid.Parse(s); return e == nil }
func ValidToken(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func canonical(v any) string { b, _ := json.Marshal(v); return Digest(string(b)) }
func other(c string) string {
	if c == "w" {
		return "b"
	}
	return "w"
}

type scanner interface{ Scan(...any) error }

const columns = `id,creator_id,creator_name,creator_color,guest_name,mode,initial_fen,current_fen,revision,status,result,reason,tags,created_at,updated_at,completed_at`

func scanMatch(row scanner) (m Match, e error) {
	e = row.Scan(&m.ID, &m.CreatorID, &m.CreatorName, &m.CreatorColor, &m.GuestName, &m.Mode, &m.InitialFEN, &m.CurrentFEN, &m.Revision, &m.Status, &m.Result, &m.Reason, &m.Tags, &m.CreatedAt, &m.UpdatedAt, &m.CompletedAt)
	return
}

func (s *Store) Create(ctx context.Context, owner, request string, setup Setup) (Created, error) {
	var out Created
	if !validID(owner) || !validID(request) || setup.Name != strings.TrimSpace(setup.Name) || utf8.RuneCountInString(setup.Name) < 1 || utf8.RuneCountInString(setup.Name) > 80 || strings.ContainsAny(setup.Name, "\r\n\x00") || (setup.Color != "w" && setup.Color != "b") {
		return out, ErrInvalid
	}
	if utf8.RuneCountInString(setup.CreatorName) > 64 || strings.ContainsAny(setup.CreatorName, "\r\n\x00") {
		return out, ErrInvalid
	}
	if setup.Mode != "standard" && setup.Mode != "chess960" && setup.Mode != "custom" {
		return out, ErrInvalid
	}
	digest := canonical(setup)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, owner+":"+request); e != nil {
		return out, e
	}
	var prior, outcome string
	e = tx.QueryRowContext(ctx, `SELECT digest,resource_id::text,outcome FROM mutation_receipts WHERE actor=$1 AND resource='create' AND request_id=$2`, owner, request).Scan(&prior, &out.ID, &outcome)
	if e == nil {
		if prior != digest {
			return out, ErrPayload
		}
		out.Replay = true
		out.Deleted = outcome == "deleted"
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	fen := setup.FEN
	if setup.Mode == "standard" {
		fen = game.StartFEN
	}
	if setup.Mode == "chess960" && setup.PGN == "" {
		fen, e = game.Random960()
		if e != nil {
			return out, e
		}
	}
	var g *game.Game
	if setup.PGN != "" {
		g, e = game.ImportPGN(setup.PGN)
		if e == nil {
			switch {
			case g.Chess960:
				setup.Mode = "chess960"
			case g.InitialFEN == game.StartFEN:
				setup.Mode = "standard"
			default:
				setup.Mode = "custom"
			}
		}
	} else {
		g, e = game.Replay(fen, nil, setup.Mode == "chess960")
	}
	if e != nil {
		return out, fmt.Errorf("%w %s", ErrInvalid, e)
	}
	if g.Chess960 {
		setup.Mode = "chess960"
	}
	status := "pending"
	var completed any
	if g.Result != "*" {
		status = "completed"
		completed = time.Now().UTC()
	}
	out.ID = uuid.NewString()
	out.PlayingToken = Token()
	out.ViewToken = Token()
	tags, _ := json.Marshal(g.Tags)
	if _, e = tx.ExecContext(ctx, `INSERT INTO matches(id,creator_id,creator_color,guest_name,mode,initial_fen,current_fen,revision,status,result,reason,tags,completed_at,creator_name) VALUES($1,$2,$3,$4,$5,$6,$7,0,$8,$9,$10,$11,$12,$13)`, out.ID, owner, setup.Color, setup.Name, setup.Mode, g.InitialFEN, g.Board.Fen(), status, g.Result, g.Reason, string(tags), completed, setup.CreatorName); e != nil {
		return Created{}, e
	}
	for i, m := range g.Moves {
		if _, e = tx.ExecContext(ctx, `INSERT INTO match_moves(match_id,ply,uci,san,fen,actor) VALUES($1,$2,$3,$4,$5,'import')`, out.ID, i+1, m.UCI, m.SAN, m.FEN); e != nil {
			return Created{}, e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO match_guest_links(match_id,token_digest,generation,seat,revoked) VALUES($1,$2,1,$3,false)`, out.ID, Digest(out.PlayingToken), other(setup.Color)); e != nil {
		return Created{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO match_view_links(match_id,token,generation,revoked) VALUES($1,$2,1,false)`, out.ID, out.ViewToken); e != nil {
		return Created{}, e
	}
	if e = receipt(ctx, tx, owner, "create", request, digest, out.ID, 0, "created"); e != nil {
		return Created{}, e
	}
	if e = tx.Commit(); e != nil {
		return Created{}, e
	}
	return out, nil
}

func receipt(ctx context.Context, tx *sql.Tx, actor, resource, request, digest, id string, revision int64, outcome string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO mutation_receipts(actor,resource,request_id,digest,resource_id,revision,outcome) VALUES($1,$2,$3,$4,$5,$6,$7)`, actor, resource, request, digest, id, revision, outcome)
	return e
}

// All grants are checked after locking their match; rotations and moves share this order.
func authorize(ctx context.Context, tx *sql.Tx, m Match, a Auth) (role, color, actor string, e error) {
	if a.Owner != "" {
		if a.Owner != m.CreatorID {
			e = ErrDenied
			return
		}
		return "owner", m.CreatorColor, a.Owner, nil
	}
	if a.Session != "" {
		var seat string
		var gen int64
		e = tx.QueryRowContext(ctx, `SELECT s.seat,s.generation FROM guest_sessions s JOIN match_guest_links g ON g.match_id=s.match_id AND g.generation=s.generation AND g.seat=s.seat WHERE s.digest=$1 AND s.match_id=$2 AND s.expires_at>clock_timestamp() AND NOT g.revoked`, Digest(a.Session), m.ID).Scan(&seat, &gen)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				e = ErrDenied
			}
			return
		}
		return "guest", seat, fmt.Sprintf("guest:%s:%d", m.ID, gen), nil
	}
	if a.View != "" {
		var ok bool
		e = tx.QueryRowContext(ctx, `SELECT true FROM match_view_links WHERE match_id=$1 AND token=$2 AND NOT revoked`, m.ID, a.View).Scan(&ok)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				e = ErrDenied
			}
			return
		}
		return "viewer", "", "view:" + Digest(a.View), nil
	}
	return "", "", "", ErrDenied
}

func (s *Store) matchTx(ctx context.Context, tx *sql.Tx, id string, a Auth, lock bool) (Snapshot, string, error) {
	var out Snapshot
	if !validID(id) {
		return out, "", ErrDenied
	}
	suffix := " FOR SHARE"
	if lock {
		suffix = " FOR UPDATE"
	}
	m, e := scanMatch(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM matches WHERE id=$1`+suffix, id))
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			e = ErrDenied
		}
		return out, "", e
	}
	role, color, actor, e := authorize(ctx, tx, m, a)
	if e != nil {
		return out, "", e
	}
	out.Match = m
	out.Role = role
	out.Color = color
	rows, e := tx.QueryContext(ctx, `SELECT ply,uci,san,fen,actor FROM match_moves WHERE match_id=$1 ORDER BY ply`, id)
	if e != nil {
		return out, "", e
	}
	for rows.Next() {
		var mv Move
		if e = rows.Scan(&mv.Ply, &mv.UCI, &mv.SAN, &mv.FEN, &mv.Actor); e != nil {
			rows.Close()
			return out, "", e
		}
		out.Moves = append(out.Moves, mv)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, "", e
	}
	e = tx.QueryRowContext(ctx, `SELECT token FROM match_view_links WHERE match_id=$1 AND NOT revoked`, id).Scan(&out.ViewToken)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, "", e
	}
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM match_viewers WHERE match_id=$1 AND seen_at>now()-interval '45 seconds'`, id).Scan(&out.Viewers)
	return out, actor, e
}
func (s *Store) Snapshot(ctx context.Context, id string, a Auth) (Snapshot, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Snapshot{}, e
	}
	defer tx.Rollback()
	out, _, e := s.matchTx(ctx, tx, id, a, false)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

// RememberCreatorName fills legacy matches only after an authenticated owner read.
func (s *Store) RememberCreatorName(ctx context.Context, id, owner, name string) error {
	if !validID(id) || !validID(owner) || name == "" || utf8.RuneCountInString(name) > 64 || strings.ContainsAny(name, "\r\n\x00") {
		return ErrInvalid
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE matches SET creator_name=$3 WHERE id=$1 AND creator_id=$2 AND creator_name=''`, id, owner, name)
	return err
}

func (s *Store) ViewID(ctx context.Context, token string) (string, error) {
	if !ValidToken(token) {
		return "", ErrDenied
	}
	var id string
	e := s.DB.QueryRowContext(ctx, `SELECT match_id FROM match_view_links WHERE token=$1 AND NOT revoked`, token).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrDenied
	}
	return id, e
}

func (s *Store) Exchange(ctx context.Context, token string) (id, session string, e error) {
	if !ValidToken(token) {
		return "", "", ErrDenied
	}
	e = s.DB.QueryRowContext(ctx, `SELECT match_id FROM match_guest_links WHERE token_digest=$1 AND NOT revoked`, Digest(token)).Scan(&id)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			e = ErrDenied
		}
		return
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	m, err := scanMatch(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM matches WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return "", "", ErrDenied
	}
	var gen int64
	var seat string
	if e = tx.QueryRowContext(ctx, `SELECT generation,seat FROM match_guest_links WHERE match_id=$1 AND token_digest=$2 AND NOT revoked`, id, Digest(token)).Scan(&gen, &seat); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			e = ErrDenied
		}
		return
	}
	session = Token()
	if _, e = tx.ExecContext(ctx, `INSERT INTO guest_sessions(digest,match_id,generation,seat,expires_at) VALUES($1,$2,$3,$4,now()+interval '30 days')`, Digest(session), id, gen, seat); e != nil {
		return
	}
	if m.Status == "pending" {
		if _, e = tx.ExecContext(ctx, `UPDATE matches SET status='active',revision=revision+1,updated_at=now() WHERE id=$1`, id); e != nil {
			return
		}
	}
	e = tx.Commit()
	return
}

func (s *Store) Mutate(ctx context.Context, id string, a Auth, in Mutation) (Snapshot, string, error) {
	if !validID(in.RequestID) || in.Revision < 0 {
		return Snapshot{}, "", ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Snapshot{}, "", e
	}
	defer tx.Rollback()
	out, actor, e := s.matchTx(ctx, tx, id, a, true)
	if e != nil {
		// A deletion receipt confirms only this owner's identical successful request.
		if in.Action == "delete" && a.Owner != "" {
			var d string
			er := tx.QueryRowContext(ctx, `SELECT digest FROM mutation_receipts WHERE actor=$1 AND resource='delete' AND request_id=$2 AND resource_id=$3`, a.Owner, in.RequestID, id).Scan(&d)
			if er == nil && d == canonical(in) {
				return out, "deleted", tx.Commit()
			}
		}
		return out, "", e
	}
	if out.Role == "viewer" {
		return out, "", ErrDenied
	}
	digest := canonical(in)
	var prior, outcome string
	e = tx.QueryRowContext(ctx, `SELECT digest,outcome FROM mutation_receipts WHERE actor=$1 AND resource=$2 AND request_id=$3`, actor, id, in.RequestID).Scan(&prior, &outcome)
	if e == nil {
		if prior != digest {
			return out, "", ErrPayload
		}
		return out, "", tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, "", e
	}
	if in.Revision != out.Revision {
		return out, "", ErrConflict
	}
	secret := ""
	outcome = in.Action
	switch in.Action {
	case "move":
		if out.Status != "active" {
			return out, "", ErrConflict
		}
		var moves []string
		for _, m := range out.Moves {
			moves = append(moves, m.UCI)
		}
		g, er := game.Replay(out.InitialFEN, moves, out.Mode == "chess960")
		if er != nil {
			return out, "", er
		}
		if (g.Board.SideToMove == 0) != (out.Color == "w") {
			return out, "", ErrDenied
		}
		mv, er := g.Apply(in.Value)
		if er != nil {
			return out, "", fmt.Errorf("%w %s", ErrInvalid, er)
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO match_moves(match_id,ply,uci,san,fen,actor) VALUES($1,$2,$3,$4,$5,$6)`, id, len(moves)+1, mv.UCI, mv.SAN, mv.FEN, out.Role); e != nil {
			return out, "", e
		}
		status := "active"
		var completed any
		if g.Result != "*" {
			status = "completed"
			completed = time.Now().UTC()
		}
		_, e = tx.ExecContext(ctx, `UPDATE matches SET current_fen=$2,result=$3,reason=$4,status=$5,completed_at=$6,revision=revision+1,updated_at=now() WHERE id=$1`, id, g.Board.Fen(), g.Result, g.Reason, status, completed)
	case "resign":
		if out.Status != "active" {
			return out, "", ErrConflict
		}
		result := "1-0"
		if out.Color == "w" {
			result = "0-1"
		}
		_, e = tx.ExecContext(ctx, `UPDATE matches SET result=$2,reason='resignation',status='completed',completed_at=now(),revision=revision+1,updated_at=now() WHERE id=$1`, id, result)
	case "guest-replace", "guest-revoke", "view-replace", "view-revoke":
		if out.Role != "owner" {
			return out, "", ErrDenied
		}
		if strings.HasPrefix(in.Action, "guest-") {
			secret = Token()
			_, e = tx.ExecContext(ctx, `UPDATE match_guest_links SET token_digest=$2,generation=generation+1,revoked=$3 WHERE match_id=$1`, id, Digest(secret), in.Action == "guest-revoke")
			if e == nil {
				_, e = tx.ExecContext(ctx, `DELETE FROM guest_sessions WHERE match_id=$1`, id)
			}
		} else {
			secret = Token()
			_, e = tx.ExecContext(ctx, `UPDATE match_view_links SET token=$2,generation=generation+1,revoked=$3 WHERE match_id=$1`, id, secret, in.Action == "view-revoke")
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE matches SET revision=revision+1,updated_at=now() WHERE id=$1`, id)
		}
	case "delete":
		if out.Role != "owner" || in.Value != "confirmed" {
			return out, "", ErrDenied
		}
		if _, e = tx.ExecContext(ctx, `UPDATE mutation_receipts SET outcome='deleted' WHERE resource='create' AND resource_id=$1`, id); e != nil {
			return out, "", e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM mutation_receipts WHERE resource=$1`, id); e != nil {
			return out, "", e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM matches WHERE id=$1`, id); e != nil {
			return out, "", e
		}
		if e = receipt(ctx, tx, actor, "delete", in.RequestID, digest, id, out.Revision+1, "deleted"); e != nil {
			return out, "", e
		}
		return out, "deleted", tx.Commit()
	default:
		return out, "", ErrInvalid
	}
	if e != nil {
		return out, "", e
	}
	if e = receipt(ctx, tx, actor, id, in.RequestID, digest, id, out.Revision+1, outcome); e != nil {
		return out, "", e
	}
	out, _, e = s.matchTx(ctx, tx, id, a, true)
	if e != nil {
		return out, "", e
	}
	if e = tx.Commit(); e != nil {
		return out, "", e
	}
	return out, secret, nil
}

type Filter struct {
	Archive                bool
	Search, Status, Before string
	Limit                  int
}

func (s *Store) List(ctx context.Context, owner string, f Filter) ([]Match, error) {
	if !validID(owner) || len(f.Search) > 80 || len(f.Before) > 100 {
		return nil, ErrInvalid
	}
	if f.Limit < 1 || f.Limit > 50 {
		f.Limit = 25
	}
	if f.Search == "" && f.Before == "" && f.Status == "" {
		states, order := []string{"pending", "active"}, "created_at"
		if f.Archive {
			states, order = []string{"completed"}, "completed_at"
		}
		var matches []Match
		err := s.repo.FindAll(ctx, &matches, rel.Where(where.And(where.Eq("creator_id", owner), where.InString("status", states))), rel.SortDesc(order), rel.SortDesc("id"), rel.Limit(f.Limit))
		return matches, err
	}
	q := `SELECT ` + columns + ` FROM matches WHERE creator_id=$1`
	args := []any{owner}
	if f.Archive {
		q += ` AND status='completed'`
	} else {
		q += ` AND status IN ('pending','active')`
	}
	if f.Status != "" {
		args = append(args, f.Status)
		q += fmt.Sprintf(` AND status=$%d`, len(args))
	}
	if f.Search != "" {
		args = append(args, "%"+strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Search)+"%")
		q += fmt.Sprintf(` AND (id::text ILIKE $%d OR guest_name ILIKE $%d)`, len(args), len(args))
	}
	if f.Before != "" {
		parts := strings.SplitN(f.Before, "|", 2)
		if len(parts) != 2 || !validID(parts[1]) {
			return nil, ErrInvalid
		}
		stamp, e := time.Parse(time.RFC3339Nano, parts[0])
		if e != nil {
			return nil, ErrInvalid
		}
		args = append(args, stamp, parts[1])
		field := "created_at"
		if f.Archive {
			field = "completed_at"
		}
		q += fmt.Sprintf(` AND (%s,id)<($%d,$%d)`, field, len(args)-1, len(args))
	}
	if f.Archive {
		q += ` ORDER BY completed_at DESC,id DESC`
	} else {
		q += ` ORDER BY created_at DESC,id DESC`
	}
	args = append(args, f.Limit)
	q += fmt.Sprintf(` LIMIT $%d`, len(args))
	rows, e := s.DB.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Match
	for rows.Next() {
		m, e := scanMatch(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Presence(ctx context.Context, id string, a Auth, viewer string) error {
	if !validID(viewer) {
		return ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	out, actor, e := s.matchTx(ctx, tx, id, a, true)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO match_viewers(match_id,viewer_id,role,scope,seen_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(match_id,viewer_id) DO UPDATE SET role=excluded.role,scope=excluded.scope,seen_at=now()`, id, viewer, out.Role, actor)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) Owned(ctx context.Context, id, owner string) (Match, error) {
	if !validID(id) || !validID(owner) {
		return Match{}, ErrDenied
	}
	var m Match
	err := s.repo.Find(ctx, &m, rel.Where(where.And(where.Eq("id", id), where.Eq("creator_id", owner))))
	return m, err
}
