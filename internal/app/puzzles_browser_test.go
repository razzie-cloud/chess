package app

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/tests/browser"
)

func TestBrowserPuzzleRejectedMovesConflictRefreshAndEndedControls(t *testing.T) {
	if os.Getenv("CHROMIUM_EXECUTABLE_PATH") == "" {
		t.Skip("set CHROMIUM_EXECUTABLE_PATH for puzzle browser acceptance")
	}
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	a, err := s.StartAttempt(t.Context(), f.owner, "razchess-mates-v1", 9, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Definition(t.Context(), a.CollectionID, a.Slot)
	if err != nil {
		t.Fatal(err)
	}
	position, err := game.ValidateFEN(d.FEN)
	if err != nil {
		t.Fatal(err)
	}
	wrong := ""
	for _, move := range position.LegalMoves() {
		if move.Uci(position) != d.Root.Move {
			wrong = move.Uci(position)
			break
		}
	}
	if wrong == "" {
		t.Fatal("fixture needs a legal non-prescribed move")
	}
	b := browserFixture(t, f)
	b.Call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `Object.defineProperty(window,'Chessboard',{configurable:true,get(){return window.testBoardFactory},set(factory){const wrapped=function(...args){const board=factory(...args);const destroy=board.destroy;board.destroy=()=>{window.testDestroyed=(window.testDestroyed||0)+1;destroy()};(window.testBoards||=new Map()).set(args[0].id,board);return board};Object.assign(wrapped,factory);window.testBoardFactory=wrapped}});`})
	b.Navigate(f.cfg.Origin + "/puzzle-attempts/" + a.ID)
	b.Run(`await wait(()=>window.testBoards?.has('board-root'));window.stableRoot=el('#board-root');assert(!document.documentElement.outerHTML.includes('replies'),'compiled solution exposed');const form=el('.puzzle-move');testBoards.get('board-root').position('start',false);form.elements.uci.value=` + browser.Quote(wrong) + `;form.requestSubmit();await wait(()=>el('#puzzle-message').textContent.includes('Not the solution move'));assert(testBoards.get('board-root').fen()===el('.branch-state').dataset.fen.split(' ')[0],'rejected provisional move remained');assert(el('#attempt-state').dataset.revision==='0','wrong move changed revision');assert(!form.querySelector('button').disabled,'failed move blocked retry');`)
	// Another tab advances the root. Posting this tab's old revision must reconcile.
	a, err = s.PuzzleMove(t.Context(), a.ID, f.owner, store.PuzzleMove{Branch: "root", ContentDigest: a.Digest, Path: []string{}, UCI: d.Root.Move, Revision: 0})
	if err != nil {
		t.Fatal(err)
	}
	b.Run(`el('.puzzle-move').elements.uci.value=` + browser.Quote(d.Root.Move) + `;el('.puzzle-move').requestSubmit();await wait(()=>document.querySelectorAll('.branch').length===3);assert(el('#attempt-state').dataset.revision==='1','conflict did not refresh while pending');assert(el('#board-root')===stableRoot,'conflict remounted root');window.sibling=el('#board-root-2');el('#branch-root-2 input[type=radio]').click();assert(el('[data-puzzle-gif]').href.endsWith('/branches/root-2/export/gif'),'selected branch export points elsewhere');window.keptDialog=el('#puzzle-promotion');el('#puzzle-promotion').showModal();`)
	for _, branch := range []string{"root-1", "root-2"} {
		move := d.Root.Replies[0].Next.Move
		if branch == "root-2" {
			move = d.Root.Replies[1].Next.Move
		}
		b.Run(`const form=el('#branch-` + branch + ` form');form.elements.uci.value=` + browser.Quote(move) + `;form.requestSubmit();await wait(()=>el('#branch-` + branch + ` .branch-state').dataset.status==='solved');assert(el('#board-root-2')===sibling,'sibling remounted');assert(el('#puzzle-promotion')===keptDialog,'promotion dialog remounted');await frame();assert(el('#branch-` + branch + ` .branch-state').dataset.check,'check square missing');assert(document.querySelector('#branch-` + branch + ` .in-check'),'mated king is not highlighted');`)
	}
	b.Run(`assert(el('#attempt-state').dataset.status==='solved','all-lines completion absent');assert(!document.querySelector('.puzzle-move'),'solved form remains');assert(el('form[action$="/end"] button').disabled,'solved attempt can resign');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	b.Run(`window.beforeDestroy=window.testDestroyed||0;el('.app-nav a[href=\"/puzzles\"]').click();await wait(()=>location.pathname==='/puzzles'&&!document.querySelector('#puzzle-panel'));assert(testDestroyed-beforeDestroy===3,'attempt boards not destroyed on navigation');el('#main a[href=\"/puzzles/razchess-mates-v1\"]').click();await wait(()=>document.querySelector('.puzzle-grid'));el('.puzzle-grid a[href$=\"/0\"]').click();await wait(()=>document.querySelector('.puzzle-preview img'));window.previewBefore=testDestroyed;el('.app-nav a[href=\"/matches/new\"]').click();await wait(()=>location.pathname==='/matches/new');assert(testDestroyed===previewBefore+1,'preview board not destroyed on navigation');`)
	// Ended attempts preserve their board and exports, with no native move form.
	ended, err := s.StartAttempt(t.Context(), f.owner, a.CollectionID, 35, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.EndAttempt(t.Context(), ended.ID, f.owner, uuid.NewString(), ended.Revision); err != nil {
		t.Fatal(err)
	}
	b.Navigate(f.cfg.Origin + "/puzzle-attempts/" + ended.ID)
	b.Run(`assert(el('#attempt-state').dataset.status==='ended','ended status absent');assert(!document.querySelector('.puzzle-move'),'ended attempt exposes native move form');assert(el('form[action$="/end"] button').disabled,'ended attempt can resign');assert(el('[data-puzzle-gif]').href.includes('/branches/root/export/gif'),'ended branch export unavailable');`)
	castling, err := s.StartAttempt(t.Context(), f.owner, a.CollectionID, 0, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	b.Navigate(f.cfg.Origin + "/puzzle-attempts/" + castling.ID)
	b.Run(`await wait(()=>document.querySelector('.puzzle-castles button'));const castle=el('.puzzle-castles button');assert(castle.textContent==='Castle e1h1','castling control does not use canonical UCI');castle.click();await wait(()=>el('#puzzle-message').textContent.includes('Not the solution move'));assert(!castle.disabled,'castling control disabled after rejection');assert(testBoards.get('board-root').fen()===el('.branch-state').dataset.fen.split(' ')[0],'rejected castle changed board');el('#branch-root [data-square=a1]').dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));el('[data-puzzle-action=flip]').click();assert(el('#branch-root [data-square=a1]').classList.contains('annotated'),'flip lost annotation');`)
	b.Viewport(390, 1000)
	b.Run(`await frame();assert(el('#branch-root [data-square=a1]').classList.contains('annotated'),'resize lost annotation');`)
	b.AssertNoErrors()
}
