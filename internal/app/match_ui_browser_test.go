package app

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/tests/browser"
)

func TestBrowserMatchCardsAndNewGameNavigation(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	created, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Alexandria — long opponent display name", Color: "b", Mode: "custom", PGN: "1. e4 e5 *"})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Archived opponent", Color: "w", Mode: "custom", PGN: "1. f3 e5 2. g4 Qh4# 0-1"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(t.Context(), created.ID, store.Auth{Owner: f.owner})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	b := browserFixture(t, f)
	b.Navigate(f.cfg.Origin + "/matches")
	b.Run(`await wait(()=>document.querySelectorAll('.match-preview img').length===32);assert(!document.querySelector('h1'),'redundant sessions heading');assert(el('.app-nav li:first-child button').textContent==='New game','new game is not first');assert(el('.match-preview').dataset.matchFen===` + browser.Quote(snapshot.CurrentFEN) + `,'card lost current position');assert(el('.match-preview [data-square=e4] img').dataset.piece==='wP','card shows initial board');const a=el('.match-preview [data-square=a8]').getBoundingClientRect(),h=el('.match-preview [data-square=h1]').getBoundingClientRect();assert(a.left>h.left&&a.top>h.top,'card ignores owner color');window.header=el('.cloud-header');`)
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 1000)
			b.Run(`await frame();assert(document.documentElement.scrollWidth<=innerWidth,'card overflow');{const input=el('.filters input').getBoundingClientRect(),searchButton=el('.filters>button').getBoundingClientRect();if(searchButton.top<input.bottom)assert(Math.abs(input.top-searchButton.top)<2&&Math.abs(input.bottom-searchButton.bottom)<2,'search controls misaligned');}const controls=[...document.querySelectorAll('.app-nav [data-nav]')].map(e=>e.getBoundingClientRect());assert(controls.every(e=>Math.abs(e.top-controls[0].top)<2&&Math.abs(e.height-controls[0].height)<2),'navigation controls misaligned '+JSON.stringify(controls));assert(el('.app-nav').getBoundingClientRect().height<65,'navigation wrapped');el('[popovertarget=new-game-menu]').click();await frame();const popup=el('#new-game-menu').getBoundingClientRect();assert(popup.left>=0&&popup.right<=innerWidth&&popup.top>=0&&popup.bottom<=innerHeight,'new game menu overflow');assert(el('#new-game-menu').querySelectorAll('a').length===4,'new game choices missing');`)
			if width == 390 || width == 1440 {
				screenshot(t, b, fmt.Sprintf("new-game-menu-%s-%d", theme, width))
			}
			b.Run(`el('#new-game-menu').hidePopover();`)
			screenshot(t, b, fmt.Sprintf("sessions-%s-%d", theme, width))
		}
	}
	b.Run(`window.preview=el('.match-preview');el('.app-nav a[href="/matches/archive"]').click();await wait(()=>location.pathname==='/matches/archive'&&document.querySelector('.match-preview [data-square=h4] img'));assert(el('.cloud-header')===header,'archive replaced shell');assert(!document.querySelector('h1'),'redundant archive heading');assert(!preview.isConnected,'session card retained');assert(el('.match-card-link').pathname===` + browser.Quote("/matches/"+archived.ID) + `,'archive link wrong');assert(el('.match-preview [data-square=h4] img').dataset.piece==='bQ','archive lost final position');assert(el('.match-card form input[name=return_to]').value==='archive','archive deletion destination lost');`)
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 1000)
			b.Run(`await frame();assert(document.documentElement.scrollWidth<=innerWidth,'archive overflow');const input=el('.filters input').getBoundingClientRect(),searchButton=el('.filters>button').getBoundingClientRect();if(searchButton.top<input.bottom)assert(Math.abs(input.top-searchButton.top)<2&&Math.abs(input.bottom-searchButton.bottom)<2,'archive search misaligned');`)
			screenshot(t, b, fmt.Sprintf("archive-%s-%d", theme, width))
		}
	}
	for _, variant := range []struct{ href, mode string }{{"/matches/new", "standard"}, {"/matches/new?mode=chess960", "chess960"}, {"/create", "custom"}} {
		b.Run(`el('[popovertarget=new-game-menu]').click();await frame();el('#new-game-menu a[href="` + variant.href + `"]').click();await wait(()=>document.querySelector('#mode')?.value===` + browser.Quote(variant.mode) + `);assert(el('.cloud-header')===header,'setup replaced shell');assert(!el('#new-game-menu').matches(':popover-open'),'menu stayed open');assert(el('[popovertarget=new-game-menu]').getAttribute('aria-current')==='page','new game marker missing');`)
	}
	b.Run(`el('[popovertarget=new-game-menu]').click();await frame();const random=el('#new-game-menu a[href="/puzzles/random"]');assert(random.textContent==='Play a random puzzle','random puzzle label');random.click();await wait(()=>/^\/puzzles\/razchess-mates-v1\/\d+$/.test(location.pathname)&&document.querySelector('.branch .board img'));assert(el('.cloud-header')===header,'random puzzle replaced shell');assert(el('#puzzle-panel').dataset.base===location.pathname,'random URL differs from puzzle');assert(!el('#new-game-menu').matches(':popover-open'),'random menu stayed open');assert(el('.app-nav a[href="/puzzles"]').getAttribute('aria-current')==='page','puzzle marker missing');el('[popovertarget=new-game-menu]').click();await frame();el('#new-game-menu a[href="/matches/new"]').click();await wait(()=>document.querySelector('#setup'));`)
	b.Run(`el('#setup').elements.name.value='Sidebar opponent';el('#setup').requestSubmit();`)
	waitDocument(t, b, `document.querySelector('#match-board img')&&document.querySelector('[data-playing-link]')`)
	b.Run(`assert(el('.match-sidebar').contains(el('#move-form')),'move form is below board');assert(el('.match-sidebar').contains(el('[data-playing-link]')),'playing link outside sidebar');assert(el('.match-sidebar').contains(el('.match-management')),'management outside sidebar');assert(el('.match-heading').contains(el('[popovertarget=game-menu]')),'game menu not prominent');assert(!el('#game-menu').querySelector('a[href$="/matches/new"],a[href*="mode=chess960"],a[href*="/create"],a[href*="/puzzles"]'),'new games remain in game menu');assert(el('.match-board-panel').children.length===1,'controls remain below board');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	b.AssertNoErrors()
}
