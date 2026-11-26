package app

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/razzie-cloud/chess/internal/bot"
)

func TestBotSetupBrowser(t *testing.T) {
	f := newHTTPFixture(t, nil)
	enableBot(t, f, nil)
	b := browserFixture(t, f)
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 1100)
			for _, path := range []string{"/matches/new", "/matches/new?mode=chess960", "/create"} {
				b.Navigate(f.cfg.Origin + path)
				b.Run(`await wait(()=>document.querySelector('#setup-opponent'));const name=el('#setup input[name=name]');name.value='Guest draft';const opponent=el('#setup-opponent');opponent.value='bot';opponent.dispatchEvent(new Event('change',{bubbles:true}));assert(!name.required,'bot still needs guest name');const level=el('[name=bot_level]');assert(!level.disabled&&visible(level)&&level.options.length===5,'difficulty choices');level.value='4';assert(!visible(name),'guest field visible for bot');assert(!new FormData(el('#setup')).has('uci'),'setup submitted a move');assert(new FormData(el('#setup')).get('bot_level')==='4','difficulty not submitted');assert(document.documentElement.scrollWidth<=innerWidth,'bot setup overflow');opponent.value='guest';opponent.dispatchEvent(new Event('change',{bubbles:true}));assert(name.required&&name.value==='Guest draft'&&level.disabled,'guest draft lost');assert(!new FormData(el('#setup')).has('bot_level'),'inactive difficulty submitted');opponent.value='bot';opponent.dispatchEvent(new Event('change',{bubbles:true}));assert(level.value==='4','difficulty lost after switch');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
				if path == "/matches/new" && (width == 390 || width == 1440) {
					screenshot(t, b, fmt.Sprintf("setup-bot-%s-%d", theme, width))
				}
				b.Run(`el('#setup').dispatchEvent(new Event('submit'));`)
			}
		}
	}
	// Navigation mounts the opponent controls again without replacing the header.
	b.Navigate(f.cfg.Origin + "/matches/new")
	b.Run(`const header=el('.cloud-header');el('[popovertarget=new-game-menu]').click();await frame();el('#new-game-menu a[href="/create"]').click();await wait(()=>document.querySelector('#editor-board')&&document.querySelector('#setup-opponent'));assert(el('.cloud-header')===header,'navigation replaced shell');el('#setup-opponent').value='bot';el('#setup-opponent').dispatchEvent(new Event('change',{bubbles:true}));el('[name=bot_level]').value='5';el('input[name=source][value=fen]').click();el('#edit-fen').value='bad FEN';el('button[formaction]').click();`)
	waitDocument(t, b, `document.querySelector('[data-error-summary]')`)
	b.Run(`assert(el('#setup-opponent').value==='bot'&&el('[name=bot_level]').value==='5','validation lost bot selection');assert(!el('[name=name]').required,'error requires guest name');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	b.AssertNoErrors()
}

func TestBotPlayBrowser(t *testing.T) {
	binary := os.Getenv("STOCKFISH_TEST_PATH")
	if binary == "" {
		t.Skip("set STOCKFISH_TEST_PATH for real bot browser play")
	}
	storage := httpStore(t)
	f := newHTTPFixture(t, storage)
	enableBot(t, f, storage)
	token := strings.Repeat("x", 32)
	callback := httptest.NewServer(BotHandler(storage, token))
	defer callback.Close()
	worker := bot.NewWorker(token, callback.URL+"/bot/moves", binary, 2)
	engine := httptest.NewServer(worker.Handler())
	defer engine.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		DispatchBots(ctx, storage, engine.URL+"/jobs", callback.URL+"/bot/moves", token)
	}()
	defer func() { cancel(); <-done; <-dispatched }()
	b := browserFixture(t, f)
	for _, variant := range []struct {
		path, color string
		level       int
	}{
		{"/matches/new", "b", 1},
		{"/matches/new?mode=chess960", "b", 3},
		{"/create", "w", 5},
	} {
		b.Navigate(f.cfg.Origin + variant.path)
		b.Run(fmt.Sprintf(`el('#setup-opponent').value='bot';el('#setup-opponent').dispatchEvent(new Event('change',{bubbles:true}));el('[name=bot_level]').value='%d';el('[name=color][value=%s]').click();el('#setup').requestSubmit();`, variant.level, variant.color))
		waitDocument(t, b, `document.querySelector('#match-board img')`)
		b.Run(fmt.Sprintf(`assert(!document.querySelector('[data-playing-link],form[action$="/guest-link"]'),'guest credentials in bot match');assert(el('h1').textContent.includes(%q),'difficulty label lost');`, bot.Name(variant.level)))
		if variant.color == "b" {
			b.Run(`await wait(()=>JSON.parse(el('#match-state').dataset.state).history?.length===1);assert(JSON.parse(el('#match-state').dataset.state).history[0].Actor==='bot','first move not from engine');`)
		} else {
			b.Run(`el('#move-form input[name=uci]').value='e2e4';el('#move-form').requestSubmit();await wait(()=>JSON.parse(el('#match-state').dataset.state).history?.length===2);assert(JSON.parse(el('#match-state').dataset.state).history[1].Actor==='bot','response missing');`)
		}
		b.Run(`assert(JSON.parse(el('#match-state').dataset.state).legal.length>0,'owner turn not unlocked');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	}
	b.AssertNoErrors()
}
