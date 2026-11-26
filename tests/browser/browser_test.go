package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitDevToolsPort(t *testing.T) {
	for _, initial := range []string{"", "123"} {
		t.Run("initial="+initial, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "DevToolsActivePort")
			if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
				t.Fatal(err)
			}
			written := make(chan error, 1)
			time.AfterFunc(100*time.Millisecond, func() {
				written <- os.WriteFile(path, []byte("12345\n/devtools/browser/test"), 0600)
			})
			port := waitDevToolsPort(path, time.Now().Add(2*time.Second))
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			if port != "12345" {
				t.Fatalf("port = %q; want 12345 from the completed first line", port)
			}
		})
	}
}

func TestWaitDevToolsPortTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "DevToolsActivePort")
	if port := waitDevToolsPort(path, time.Now().Add(50*time.Millisecond)); port != "" {
		t.Fatalf("port = %q for a missing file", port)
	}
}
