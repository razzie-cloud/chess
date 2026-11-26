package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/tests/browser"
)

func TestBrowserWinFireworks(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	for _, tc := range []struct {
		name, color, fen, pgn, move string
		remote, reduced, draw       bool
	}{
		{name: "white win", color: "w", fen: "7k/8/5KQ1/8/8/8/8/8 w - - 0 1", move: "g6g7"},
		{name: "black win", color: "b", pgn: "1. f3 e5 2. g4 *", move: "d8h4"},
		{name: "remote win", color: "b", fen: "7k/8/5KQ1/8/8/8/8/8 w - - 0 1", move: "g6g7", remote: true},
		{name: "draw", color: "w", fen: "7k/8/5KQ1/8/8/8/8/8 w - - 0 1", move: "g6f7", draw: true},
		{name: "reduced motion", color: "w", fen: "7k/8/5KQ1/8/8/8/8/8 w - - 0 1", move: "g6g7", reduced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Opponent", Color: tc.color, Mode: "custom", FEN: tc.fen, PGN: tc.pgn})
			if err != nil {
				t.Fatal(err)
			}
			_, session, err := s.Exchange(t.Context(), created.PlayingToken)
			if err != nil {
				t.Fatal(err)
			}
			b := browserFixture(t, f)
			if tc.reduced {
				b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": "reduce"}}})
			}
			url := f.cfg.Origin + "/matches/" + created.ID
			b.Navigate(url)
			b.Run(`assert(!document.querySelector('.fireworks'),'unfinished game celebrated');window.bursts=0;const play=fireworks.play;fireworks.play=()=>{bursts++;play()};`)
			if tc.remote {
				auth := store.Auth{Session: session}
				snap, err := s.Snapshot(t.Context(), created.ID, auth)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.Mutate(t.Context(), created.ID, auth, store.Mutation{RequestID: uuid.NewString(), Revision: snap.Revision, Action: "move", Value: tc.move}); err != nil {
					t.Fatal(err)
				}
			} else {
				b.Run(`el('#move-form').elements.uci.value=` + browser.Quote(tc.move) + `;el('#move-form').requestSubmit();`)
			}
			b.Run(`await wait(()=>JSON.parse(el('#match-state').dataset.state).status==='completed');`)
			if tc.draw || tc.reduced {
				b.Run(`assert(!document.querySelector('.fireworks'),'draw or reduced motion animated');`)
			} else {
				b.Run(`const canvas=el('.fireworks');assert(bursts===1,'win celebrated more than once');assert(canvas.getAttribute('aria-hidden')==='true','decorative canvas exposed');assert(getComputedStyle(canvas).pointerEvents==='none','canvas blocks controls');await frame();assert(canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data.some(value=>value!==0),'fireworks did not draw');el('[data-live]').click();assert(bursts===1,'unchanged state replayed fireworks');await wait(()=>!document.querySelector('.fireworks'));assert(cspErrors.length===0,'fireworks violated CSP');`)
			}
			b.Navigate(url)
			b.Run(`assert(!document.querySelector('.fireworks'),'reopened archive celebrated');`)
			if tc.name == "white win" {
				for _, theme := range []string{"light", "dark"} {
					b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
					for _, width := range []int{320, 390, 768, 1440} {
						b.Viewport(width, 1000)
						b.Run(`await frame();el('[popovertarget=game-menu]').focus();fireworks.play();await frame();const canvas=el('.fireworks'),box=canvas.getBoundingClientRect();assert(box.width===document.documentElement.clientWidth&&box.height===innerHeight,'canvas size differs from viewport');assert(document.documentElement.scrollWidth<=innerWidth,'fireworks overflow');assert(document.activeElement===el('[popovertarget=game-menu]'),'fireworks stole focus');`)
						if width == 390 || width == 1440 {
							fireworksScreenshot(t, b, fmt.Sprintf("fireworks-%s-%d", theme, width))
						}
					}
				}
				b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": "reduce"}}})
				b.Run(`await wait(()=>!document.querySelector('.fireworks'));fireworks.play();assert(!document.querySelector('.fireworks'),'reduced motion started fireworks');`)
				b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": "no-preference"}}})
				b.Run(`fireworks.play();assert(document.querySelector('.fireworks'),'motion preference did not update');el('.app-nav a[href="/matches"]').click();await wait(()=>location.pathname==='/matches'&&!document.querySelector('#match-panel'));assert(!document.querySelector('.fireworks'),'navigation kept fireworks');assert(cspErrors.length===0,'fireworks violated CSP');`)
			}
			b.AssertNoErrors()
		})
	}
}

func fireworksScreenshot(t *testing.T, b *browser.Browser, name string) {
	t.Helper()
	// Capture visible bursts without the full-page resize cancelling the effect.
	b.Run(`await new Promise(resolve=>setTimeout(resolve,700));assert(document.querySelector('.fireworks'),'fireworks stopped before capture');`)
	var result struct{ Data string }
	if err := json.Unmarshal(b.Call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}), &result); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("..", "..", "docs", "screenshots", name+".png"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
