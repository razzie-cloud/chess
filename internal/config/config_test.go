package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{"ORIGIN": "http://chess.localhost:8080", "SHARE_ORIGIN": "http://shared-chess.localhost:8080", "ACCESS_GATEWAY_URL": "http://access-gateway:8080", "POSTGRES_URI": "postgres://chess:password@localhost/chess"}
}

func TestLoadOriginsAndFlags(t *testing.T) {
	values := validEnv()
	c, err := Load([]string{"--port", "9090", "--log-level", "debug"}, func(k string) string { return values[k] })
	if err != nil || c.Port != "9090" || c.LogLevel != "DEBUG" || c.Address() != ":9090" {
		t.Fatalf("flag precedence: %+v, %v", c, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ORIGIN", "http://chess.localhost/"}, {"ORIGIN", "http://user:pass@chess.localhost"},
		{"ORIGIN", "http://chess.localhost?"}, {"ORIGIN", "http://chess.localhost:70000"},
		{"ORIGIN", "http://chess.localhost#"},
		{"ORIGIN", "http://chess.localhost:"}, {"SHARE_ORIGIN", "http://CHESS.localhost.:9000"},
		{"SHARE_ORIGIN", ""}, {"ACCESS_GATEWAY_URL", ""}, {"SERVICE_PORT", "0"}, {"LOG_LEVEL", "trace"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.value
			if _, err := Load(nil, func(k string) string { return env[k] }); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestDirectURIWinsAndPasswordFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("secret@with:reserved\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Config{PostgresURI: "postgres://chess:old@localhost/chess", PostgresURIFrom: "invalid", BrokerTokenFile: "does-not-exist", PasswordFile: path}
	got, err := c.DatabaseURI(t.Context())
	if err != nil || !strings.Contains(got, "secret%40with%3Areserved@") {
		t.Fatalf("direct database config: %q %v", got, err)
	}
}

func TestAuthenticatedDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	token := "v1.chess.1." + strings.Repeat("A", 43)
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("client") != "chess" || r.URL.Query().Get("token") != token {
			t.Error("missing scoped token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte("postgres://chess:fixture@localhost/chess"))
	}))
	defer server.Close()
	c := Config{PostgresURIFrom: server.URL + "/v1/instances/postgres/chess/uri?client=chess", BrokerTokenFile: path}
	got, err := c.DatabaseURI(t.Context())
	if err != nil || got != "postgres://chess:fixture@localhost/chess" {
		t.Fatalf("discovery: %q %v", got, err)
	}
}

func TestBotConfiguration(t *testing.T) {
	base := validEnv()
	base["BOT_URL"] = "http://stockfish:8080/jobs"
	base["BOT_WEBHOOK_URL"] = "http://chess:8082/bot/moves"
	base["BOT_TOKEN"] = strings.Repeat("x", 32)
	if _, err := Load(nil, func(k string) string { return base[k] }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"BOT_TOKEN", "short"}, {"BOT_TOKEN", "12345678901234567890123456789012\n"},
		{"BOT_WEBHOOK_URL", "http://user:password@chess:8082/bot/moves"},
		{"BOT_URL", "http://stockfish:70000/jobs"}, {"BOT_URL", "http://stockfish/jobs#"},
		{"BOT_PORT", "08080"}, {"BOT_PORT", "0"}, {"BOT_WEBHOOK_URL", ""},
	} {
		env := make(map[string]string)
		for k, v := range base {
			env[k] = v
		}
		env[tc.key] = tc.value
		if _, err := Load(nil, func(k string) string { return env[k] }); err == nil {
			t.Fatalf("accepted %s", tc.key)
		}
	}
	file := filepath.Join(t.TempDir(), "bot-token")
	if err := os.WriteFile(file, []byte(strings.Repeat("y", 32)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base["BOT_TOKEN_FILE"] = file
	c, err := Load(nil, func(k string) string { return base[k] })
	if err != nil || c.BotToken != strings.Repeat("y", 32) {
		t.Fatalf("token file: %v", err)
	}
}
