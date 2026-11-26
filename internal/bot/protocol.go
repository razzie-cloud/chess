// Package bot defines the private engine API and bounded Stockfish worker.
package bot

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Request struct {
	ID      string `json:"id"`
	PGN     string `json:"pgn"`
	Level   int    `json:"level"`
	Webhook string `json:"webhook"`
}

type Result struct {
	ID  string `json:"id"`
	UCI string `json:"uci"`
}

type Difficulty struct {
	Level  int
	Name   string
	Skill  int
	Millis int
}

var Difficulties = []Difficulty{
	{1, "Beginner", 0, 100},
	{2, "Easy", 3, 200},
	{3, "Medium", 7, 350},
	{4, "Hard", 12, 600},
	{5, "Expert", 20, 1000},
}

func Name(level int) string {
	if level < 1 || level > len(Difficulties) {
		return ""
	}
	return "Stockfish · " + Difficulties[level-1].Name
}

func Authorized(r *http.Request, token string) bool {
	return len(token) >= 32 && len(r.Header.Values("Authorization")) == 1 && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1
}

// Decode rejects trailing values and unknown fields as well as oversized bodies.
func Decode(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func ValidURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.ContainsAny(raw, "\r\n#") || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

func Client() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
