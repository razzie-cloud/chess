package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/config"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/gatewayidentity"
	"github.com/razzie-cloud/chess/internal/health"
	"github.com/razzie-cloud/chess/internal/logging"
	"github.com/razzie-cloud/chess/internal/opening"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/internal/web"
)

type Server struct {
	cfg                  config.Config
	store                *store.Store
	verifier             *gatewayidentity.Verifier
	health               *health.Monitor
	ownerHost, shareHost string
	protected, public    *http.ServeMux
}
type key struct{}
type identity struct {
	ID, Name string
	Admin    bool
}
type Page struct {
	Title, Error, Notice, PlayingURL, ViewURL, Base, Opening, RequestID, Next string
	Search, FilterStatus                                                      string
	Origin, ShareOrigin, Home, Account                                        string
	User                                                                      identity
	Matches                                                                   []store.Match
	Match                                                                     *store.Snapshot
	State                                                                     *State
	Setup                                                                     store.Setup
	Archive, Editor, Bootstrap                                                bool
	Puzzle                                                                    any
}
type State struct {
	ID          string       `json:"id"`
	Revision    int64        `json:"revision"`
	FEN         string       `json:"fen"`
	Color       string       `json:"color"`
	Status      string       `json:"status"`
	Result      string       `json:"result"`
	Opening     string       `json:"opening"`
	Legal       []string     `json:"legal"`
	History     []store.Move `json:"history"`
	Viewers     int          `json:"viewers"`
	ViewURL     string       `json:"viewURL"`
	Turn        string       `json:"turn"`
	CheckSquare string       `json:"checkSquare"`
}

func New(cfg config.Config, s *store.Store, v *gatewayidentity.Verifier, h *health.Monitor) (http.Handler, error) {
	if v == nil {
		return nil, errors.New("signed gateway identity is required")
	}
	u, e := url.Parse(cfg.Origin)
	if e != nil {
		return nil, e
	}
	shared, e := url.Parse(cfg.ShareOrigin)
	if e != nil {
		return nil, e
	}
	a := &Server{cfg: cfg, store: s, verifier: v, health: h, ownerHost: u.Host, shareHost: shared.Host, protected: http.NewServeMux(), public: http.NewServeMux()}
	a.routes()
	return logging.Middleware(a), nil
}
func (s *Server) routes() {
	p, g := s.protected, s.public
	static := http.FileServer(http.FS(web.Static()))
	p.Handle("GET /static/", http.StripPrefix("/static/", static))
	g.Handle("GET /static/", http.StripPrefix("/static/", static))
	p.HandleFunc("GET /{$}", s.list)
	p.HandleFunc("GET /matches", s.list)
	p.HandleFunc("GET /matches/archive", s.list)
	p.HandleFunc("GET /matches/new", s.setup)
	p.HandleFunc("POST /matches", s.create)
	p.HandleFunc("GET /create", s.editor)
	p.HandleFunc("POST /create/validate", s.validate)
	p.HandleFunc("GET /matches/{id}", s.matchPage)
	p.HandleFunc("GET /matches/{id}/state", s.matchState)
	for _, action := range []string{"moves", "resign", "guest-link", "view-link", "delete"} {
		p.HandleFunc("POST /matches/{id}/"+action, s.mutate)
	}
	p.HandleFunc("POST /matches/{id}/presence", s.presence)
	g.HandleFunc("GET /g", s.bootstrap)
	g.HandleFunc("POST /guest-sessions", s.exchange)
	for _, base := range []string{"play/{id}", "watch/{token}"} {
		g.HandleFunc("GET /"+base, s.matchPage)
		g.HandleFunc("GET /"+base+"/state", s.matchState)
		g.HandleFunc("POST /"+base+"/presence", s.presence)
		for _, format := range []string{"fen", "pgn", "gif"} {
			g.HandleFunc("GET /"+base+"/export/"+format, s.export)
		}
	}
	g.HandleFunc("POST /play/{id}/moves", s.mutate)
	g.HandleFunc("POST /play/{id}/resign", s.mutate)
	for _, format := range []string{"fen", "pgn", "gif"} {
		p.HandleFunc("GET /matches/{id}/export/"+format, s.export)
	}
	s.puzzleRoutes(p)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", `default-src 'none'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; media-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'`)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if r.Host != s.ownerHost && r.Host != s.shareHost {
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if (host == "localhost" || ip != nil && (ip.IsLoopback() || ip.IsPrivate())) && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") {
			s.health.Handle(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		origin := s.cfg.Origin
		if r.Host == s.shareHost {
			origin = s.cfg.ShareOrigin
		}
		if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != origin {
			http.Error(w, "This form must be submitted from Chess.", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 300<<10)
		if e := r.ParseForm(); e != nil {
			http.Error(w, "The form is too large or invalid.", 422)
			return
		}
	}
	if r.Host == s.shareHost {
		_, pattern := s.public.Handler(r)
		logging.SetRoute(r.Context(), pattern)
		s.public.ServeHTTP(w, r)
		return
	}
	if s.verifier.Verify(r, false) != nil {
		w.Header().Set("HX-Refresh", "true")
		http.Error(w, "Your cloud session expired. Sign in again.", 401)
		return
	}
	id := r.Header.Get("X-Auth-Request-User")
	if _, e := uuid.Parse(id); e != nil {
		http.Error(w, "Verified identity required.", 403)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), key{}, identity{id, r.Header.Get("X-Auth-Request-Name"), s.verifier.IsAdmin(r)}))
	_, pattern := s.protected.Handler(r)
	logging.SetRoute(r.Context(), pattern)
	s.protected.ServeHTTP(w, r)
}

func (s *Server) page(r *http.Request, title string) Page {
	u, _ := r.Context().Value(key{}).(identity)
	return Page{Title: title, User: u, Origin: s.cfg.Origin, ShareOrigin: s.cfg.ShareOrigin, Home: s.cfg.LandingOrigin, Account: s.verifier.AccountURL(s.cfg.LandingOrigin), RequestID: uuid.NewString()}
}
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p Page, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "HX-Request")
	var b bytes.Buffer
	if e := web.Render(&b, name, p); e != nil {
		logging.Fail(r.Context(), slog.LevelError, "render", e)
		http.Error(w, "Chess is temporarily unavailable.", 503)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, e error) {
	status := 503
	msg := "Chess is temporarily unavailable. Please retry."
	switch {
	case errors.Is(e, store.ErrDenied):
		status = 404
		msg = e.Error()
	case errors.Is(e, store.ErrConflict), errors.Is(e, store.ErrPayload):
		status = 409
		msg = e.Error()
	case errors.Is(e, store.ErrInvalid):
		status = 422
		msg = e.Error()
	case errors.Is(e, store.ErrDeleted):
		status = 410
		msg = e.Error()
	default:
		logging.Fail(r.Context(), slog.LevelError, "request", e)
	}
	p := s.page(r, "Chess")
	p.Error = msg
	s.render(w, r, "error", p, status)
}
func navigate(w http.ResponseWriter, r *http.Request, dest string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(200)
	} else {
		http.Redirect(w, r, dest, 303)
	}
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Sessions")
	p.Archive = strings.HasSuffix(r.URL.Path, "/archive")
	if p.Archive {
		p.Title = "Archive"
	}
	f := store.Filter{Archive: p.Archive, Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), Before: r.URL.Query().Get("before"), Limit: 25}
	p.Search, p.FilterStatus, p.Base = f.Search, f.Status, r.URL.Path
	rows, e := s.store.List(r.Context(), p.User.ID, f)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	p.Matches = rows
	if len(rows) == 25 {
		m := rows[len(rows)-1]
		stamp := m.CreatedAt
		if p.Archive && m.CompletedAt != nil {
			stamp = *m.CompletedAt
		}
		q := r.URL.Query()
		q.Set("before", stamp.Format(time.RFC3339Nano)+"|"+m.ID)
		p.Next = r.URL.Path + "?" + q.Encode()
	}
	s.render(w, r, "list", p, 200)
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "New match")
	p.Setup = store.Setup{Color: "w", Mode: "standard", FEN: game.StartFEN}
	if r.URL.Query().Get("mode") == "chess960" {
		p.Setup.Mode = "chess960"
	}
	s.render(w, r, "setup", p, 200)
}
func setupForm(r *http.Request) store.Setup {
	return store.Setup{Name: r.FormValue("name"), Color: r.FormValue("color"), Mode: r.FormValue("mode"), FEN: r.FormValue("fen"), PGN: r.FormValue("pgn")}
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "New match")
	p.Setup = setupForm(r)
	p.Setup.CreatorName = p.User.Name
	p.RequestID = r.FormValue("request_id")
	result, e := s.store.Create(r.Context(), p.User.ID, p.RequestID, p.Setup)
	if e != nil {
		if errors.Is(e, store.ErrInvalid) {
			p.Error = e.Error()
			s.render(w, r, "setup", p, 422)
		} else {
			s.fail(w, r, e)
		}
		return
	}
	if result.Deleted {
		s.fail(w, r, store.ErrDeleted)
		return
	}
	if result.PlayingToken == "" {
		navigate(w, r, "/matches/"+result.ID+"?created=1")
		return
	}
	snap, e := s.store.Snapshot(r.Context(), result.ID, store.Auth{Owner: p.User.ID})
	if e != nil {
		s.fail(w, r, e)
		return
	}
	p = s.matchData(r, snap)
	p.PlayingURL = s.cfg.ShareOrigin + "/g#" + result.PlayingToken
	p.Notice = "Match created. Anyone with this playing link can play the guest side. Save it now; it is shown only once."
	s.render(w, r, "match", p, 200)
}
func (s *Server) editor(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Create custom game")
	p.Editor = true
	p.Setup = store.Setup{Color: "w", Mode: "custom", FEN: game.StartFEN}
	if fen := r.URL.Query().Get("fen"); fen != "" {
		if b, e := game.ValidateFEN(fen); e == nil {
			p.Setup.FEN = b.Fen()
		}
	}
	s.render(w, r, "setup", p, 200)
}
func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Create custom game")
	p.Editor = true
	p.Setup = setupForm(r)
	p.Setup.Mode = "custom"
	var g *game.Game
	var e error
	if p.Setup.PGN != "" {
		g, e = game.ImportPGN(p.Setup.PGN)
	} else {
		g, e = game.New(p.Setup.FEN, false)
	}
	status := 200
	if e != nil {
		p.Error = e.Error()
		status = 422
	} else {
		p.Setup.FEN = g.Board.Fen()
		p.Notice = "Position and move history validated."
	}
	s.render(w, r, "setup", p, status)
}

func (s *Server) auth(r *http.Request) (string, store.Auth, error) {
	id := r.PathValue("id")
	a := store.Auth{}
	if r.Host == s.ownerHost {
		u, _ := r.Context().Value(key{}).(identity)
		a.Owner = u.ID
	} else if token := r.PathValue("token"); token != "" {
		a.View = token
		var e error
		id, e = s.store.ViewID(r.Context(), token)
		if e != nil {
			return "", a, e
		}
	} else {
		if c, e := r.Cookie(cookieName(id)); e == nil && store.ValidToken(c.Value) {
			a.Session = c.Value
		}
	}
	return id, a, nil
}
func cookieName(id string) string { return "chess_guest_" + strings.ReplaceAll(id, "-", "") }
func (s *Server) get(r *http.Request) (store.Snapshot, error) {
	id, a, e := s.auth(r)
	if e != nil {
		return store.Snapshot{}, e
	}
	m, e := s.store.Snapshot(r.Context(), id, a)
	if e == nil && m.Role == "owner" && m.CreatorName == "" {
		u, _ := r.Context().Value(key{}).(identity)
		e = s.store.RememberCreatorName(r.Context(), id, u.ID, u.Name)
		m.CreatorName = u.Name
	}
	return m, e
}
func (s *Server) matchData(r *http.Request, m store.Snapshot) Page {
	host := m.CreatorName
	if host == "" {
		host = "Host"
	}
	p := s.page(r, host+" vs "+m.GuestName)
	p.Match = &m
	base := "/matches/" + m.ID
	if m.Role == "guest" {
		base = "/play/" + m.ID
	}
	if m.Role == "viewer" {
		base = "/watch/" + r.PathValue("token")
	}
	p.Base = base
	st := State{ID: m.ID, Revision: m.Revision, FEN: m.CurrentFEN, Color: m.Color, Status: m.Status, Result: m.Result, History: m.Moves, Viewers: m.Viewers}
	if m.ViewToken != "" {
		p.ViewURL = s.cfg.ShareOrigin + "/watch/" + m.ViewToken
		st.ViewURL = p.ViewURL
	}
	var moves []string
	for _, mv := range m.Moves {
		moves = append(moves, mv.UCI)
	}
	if opening := opening.Lookup(m.InitialFEN, moves, m.Mode); opening != nil {
		st.Opening = opening.Code + " · " + opening.Title
		p.Opening = st.Opening
	}
	if board, err := game.ValidateFEN(m.CurrentFEN); err == nil {
		st.Turn = "White"
		if board.SideToMove == 1 {
			st.Turn = "Black"
		}
		if check, _ := board.IsCheckOrMate(); check {
			for sq, piece := range board.Piece {
				if piece.Type() == chess.King && piece.Color() == board.SideToMove {
					st.CheckSquare = chess.Sq(sq).String()
				}
			}
		}
		if m.Status == "active" && m.Color != "" && (board.SideToMove == 0) == (m.Color == "w") {
			for _, mv := range board.LegalMoves() {
				st.Legal = append(st.Legal, mv.Uci(board))
			}
		}
	}
	p.State = &st
	return p
}

func (s *Server) matchPage(w http.ResponseWriter, r *http.Request) {
	m, e := s.get(r)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	p := s.matchData(r, m)
	if r.URL.Query().Get("created") == "1" {
		p.Notice = "Created successfully. Replace the playing link to obtain a new link if you did not save it."
	}
	s.render(w, r, "match", p, 200)
}
func (s *Server) matchState(w http.ResponseWriter, r *http.Request) {
	m, e := s.get(r)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	if r.URL.Query().Get("revision") == strconv.FormatInt(m.Revision, 10) && r.URL.Query().Get("viewers") == strconv.Itoa(m.Viewers) {
		w.WriteHeader(204)
		return
	}
	s.render(w, r, "state", s.matchData(r, m), 200)
}
func (s *Server) mutate(w http.ResponseWriter, r *http.Request) {
	id, a, e := s.auth(r)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	rev, e := strconv.ParseInt(r.FormValue("revision"), 10, 64)
	if e != nil {
		s.fail(w, r, store.ErrInvalid)
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	value := r.FormValue("uci")
	switch action {
	case "moves":
		action = "move"
	case "guest-link":
		action = "guest-" + r.FormValue("action")
	case "view-link":
		action = "view-" + r.FormValue("action")
	case "delete":
		value = r.FormValue("confirm")
	}
	m, secret, e := s.store.Mutate(r.Context(), id, a, store.Mutation{RequestID: r.FormValue("request_id"), Revision: rev, Action: action, Value: value})
	if e != nil {
		s.fail(w, r, e)
		return
	}
	if action == "delete" {
		dest := "/matches"
		if r.FormValue("return_to") == "archive" {
			dest = "/matches/archive"
		}
		navigate(w, r, dest)
		return
	}
	p := s.matchData(r, m)
	if action == "guest-replace" && store.ValidToken(secret) {
		p.PlayingURL = s.cfg.ShareOrigin + "/g#" + secret
		p.Notice = "Playing link replaced. Previous guest sessions are revoked. The viewing link is independent."
	}
	if action == "guest-replace" && secret == "" {
		p.Notice = "The replacement committed. Replace the playing link again if its response was lost."
	}
	if r.Header.Get("HX-Request") == "true" && action == "move" {
		s.render(w, r, "state", p, 200)
	} else {
		s.render(w, r, "match", p, 200)
	}
}
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "Join a match")
	p.Bootstrap = true
	s.render(w, r, "bootstrap", p, 200)
}
func (s *Server) exchange(w http.ResponseWriter, r *http.Request) {
	id, session, e := s.store.Exchange(r.Context(), r.FormValue("token"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName(id), Value: session, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.cfg.ShareOrigin, "https:"), SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"url": "/play/" + id})
		return
	}
	navigate(w, r, "/play/"+id)
}
func (s *Server) presence(w http.ResponseWriter, r *http.Request) {
	id, a, e := s.auth(r)
	if e == nil {
		e = s.store.Presence(r.Context(), id, a, r.FormValue("viewer_id"))
	}
	if e != nil {
		s.fail(w, r, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	m, e := s.get(r)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	var moves []string
	for _, mv := range m.Moves {
		moves = append(moves, mv.UCI)
	}
	g, e := game.Replay(m.InitialFEN, moves, m.Mode == "chess960")
	if e != nil {
		s.fail(w, r, e)
		return
	}
	g.Result = m.Result
	_ = json.Unmarshal(m.Tags, &g.Tags)
	format := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="chess.%s"`, format))
	switch format {
	case "fen":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, m.CurrentFEN+"\n")
	case "pgn":
		w.Header().Set("Content-Type", "application/x-chess-pgn")
		_, _ = io.WriteString(w, game.ExportPGN(g))
	case "gif":
		s.writeGIF(w, r, g)
	}
}
