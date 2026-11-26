package app

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/tests/browser"
)

func TestBrowserPuzzleNumberingAndBoardTooltips(t *testing.T) {
	if os.Getenv("CHROMIUM_EXECUTABLE_PATH") == "" {
		t.Skip("set CHROMIUM_EXECUTABLE_PATH for puzzle browser acceptance")
	}
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	b := browserFixture(t, f)
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 1000)
			b.Navigate(f.cfg.Origin + "/puzzles")
			b.Call("Page.bringToFront", nil)

			b.Run(`const links=[...el('.puzzle-grid').querySelectorAll('a')];assert(links.length===206,'collection size');for(let i=0;i<links.length;i++)assert(links[i].textContent.trim()===String(i+1),'puzzle numbering');links[50].focus();await wait(()=>!el('#puzzle-tooltip').hidden&&document.querySelector('#puzzle-tooltip img'));const tip=el('#puzzle-tooltip').getBoundingClientRect();assert(tip.left>=0&&tip.right<=document.documentElement.clientWidth&&tip.top>=0&&tip.bottom<=innerHeight,'tooltip overflow');assert(links[50].getAttribute('aria-describedby')==='puzzle-tooltip','keyboard tooltip');`)
			if width == 390 || width == 1440 {
				puzzleTooltipScreenshot(t, b, "puzzle-collection-"+theme+"-"+strconv.Itoa(width))
			}
			b.Run(`const links=[...el('.puzzle-grid').querySelectorAll('a')];document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));assert(el('#puzzle-tooltip').hidden,'escape did not hide tooltip');links[50].blur();links[50].dispatchEvent(new MouseEvent('mouseenter'));assert(!el('#puzzle-tooltip').hidden,'hover preview absent');links[50].dispatchEvent(new MouseEvent('mouseleave'));assert(el('#puzzle-tooltip').hidden,'hover preview remained');window.shell=el('header');links[50].click();await wait(()=>document.querySelector('.branch .board img'));assert(el('h1').textContent==='Mate challenge 51','detail number');assert(location.pathname.endsWith('/50'),'puzzle ID');assert(el('#puzzle-state').dataset.revision==='0'&&document.querySelector('.puzzle-move'),'not immediately playable');assert(!document.querySelector('#puzzle-menu'),'puzzle menu remains');assert(el('header')===shell,'navigation replaced shell');assert(document.documentElement.scrollWidth<=innerWidth,'puzzle overflow at ` + strconv.Itoa(width) + `');`)
		}
	}
	b.AssertNoErrors()
}
func TestBrowserPuzzleFullStateRejectedMovesLocksAndNextUndone(t *testing.T) {
	if os.Getenv("CHROMIUM_EXECUTABLE_PATH") == "" {
		t.Skip("set CHROMIUM_EXECUTABLE_PATH for puzzle browser acceptance")
	}
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	d, err := s.Definition(t.Context(), "razchess-mates-v1", 9)
	if err != nil {
		t.Fatal(err)
	}
	position, err := game.ValidateFEN(d.FEN)
	if err != nil {
		t.Fatal(err)
	}
	wrong := ""
	for _, m := range position.LegalMoves() {
		if m.Uci(position) != d.Root.Move {
			wrong = m.Uci(position)
			break
		}
	}
	if wrong == "" {
		t.Fatal("fixture needs non-prescribed move")
	}
	b := browserFixture(t, f)
	b.Call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `Object.defineProperty(window,'Chessboard',{configurable:true,get(){return window.testBoardFactory},set(factory){const wrapped=function(...args){const board=factory(...args);const destroy=board.destroy;board.destroy=()=>{window.testDestroyed=(window.testDestroyed||0)+1;destroy()};(window.testBoards||=new Map()).set(typeof args[0]==='string'?args[0]:args[0].id,board);return board};Object.assign(wrapped,factory);window.testBoardFactory=wrapped}});window.moveBodies=[];const realFetch=window.fetch;window.fetch=(url,options)=>{if(options?.method==='POST'&&String(url).endsWith('/moves'))window.moveBodies.push(JSON.parse(options.body));return realFetch(url,options)};`})
	b.Navigate(f.cfg.Origin + "/puzzles/razchess-mates-v1/9")
	b.Run(`await wait(()=>window.testBoards?.has('board-root'));const form=el('.puzzle-move');testBoards.get('board-root').position('start',false);form.elements.uci.value=` + browser.Quote(wrong) + `;form.requestSubmit();await wait(()=>el('#puzzle-message').textContent.includes('Not the solution move'));assert(testBoards.get('board-root').fen()===el('.branch-state').dataset.fen.split(' ')[0],'rejected provisional move remained');assert(el('#puzzle-state').dataset.revision==='0','wrong move advanced');assert(!form.querySelector('button').disabled,'failed retry blocked');form.elements.uci.value=` + browser.Quote(d.Root.Move) + `;form.requestSubmit();await wait(()=>document.querySelectorAll('.branch').length===2);window.sibling=el('#board-root-2');window.keptDialog=el('#puzzle-promotion');assert(moveBodies.at(-1).state.boards.length===1&&moveBodies.at(-1).state.moves.length===0,'initial full state missing');assert(document.querySelectorAll('#puzzle-overview').length===1,'fork duplicated overview');assert(el('#branch-root-1 .puzzle-sidebar').contains(el('#puzzle-message')),'fork lost status sidebar');assert(!document.querySelector('article.branch'),'puzzle background tile remains');`)
	b.Viewport(1440, 1000)
	b.Run(`await frame();`)
	screenshot(t, b, "puzzle-partial-light-desktop")

	for i, branch := range []string{"root-1", "root-2"} {
		b.Run(`const form=el('#branch-` + branch + ` form');form.elements.uci.value=` + browser.Quote(d.Root.Replies[i].Next.Move) + `;form.requestSubmit();await wait(()=>el('#branch-` + branch + ` .branch-state').dataset.done==='true');assert(el('#board-root-2')===sibling,'sibling remounted');assert(el('#puzzle-promotion')===keptDialog,'dialog remounted');assert(!document.querySelector('#branch-` + branch + ` form'),'completed board can move');await frame();assert(document.querySelector('#branch-` + branch + ` .in-check'),'mated king is not red');assert(moveBodies.at(-1).state.boards.length===2,'all boards not submitted');`)
		if i == 0 {
			b.Run(`assert(!document.querySelector('.fireworks'),'partial puzzle celebrated');`)
		} else {
			b.Run(`assert(document.querySelector('.fireworks'),'all-lines completion did not celebrate');`)
		}
	}
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{390, 1440} {
			b.Viewport(width, 1000)
			b.Run(`await wait(()=>document.documentElement.scrollWidth<=innerWidth);const boards=[...document.querySelectorAll('.branch .board')];assert(document.documentElement.scrollWidth<=innerWidth,'solved puzzle overflow');if(innerWidth>700){const first=boards[0].getBoundingClientRect(),second=boards[1].getBoundingClientRect();assert(second.top>first.bottom&&Math.abs(first.left-second.left)<2&&Math.abs(first.width-second.width)<2,'locked board alignment');}`)
			screenshot(t, b, "puzzle-done-"+theme+"-"+strconv.Itoa(width))
		}
	}

	b.Run(`assert(el('#puzzle-state').dataset.done==='true','all-lines completion absent');assert(el('.puzzle-summary').textContent.includes('Puzzle done'),'done label');assert(el('[data-next-puzzle]').getAttribute('href')==='/puzzles/razchess-mates-v1/10','next undone destination');window.beforeDestroy=testDestroyed||0;window.shell=el('header');el('[data-next-puzzle]').click();await wait(()=>location.pathname.endsWith('/10')&&document.querySelector('#board-root img'));assert(el('header')===shell,'next navigation replaced shell');assert(testDestroyed-beforeDestroy===2,'completed boards not cleaned');el('.app-nav a[href="/puzzles"]').click();await wait(()=>document.querySelector('.puzzle-grid'));assert(document.querySelector('.puzzle-grid a.solved[href$="/9"]'),'durable done bit absent');`)
	b.Navigate(f.cfg.Origin + "/puzzles/razchess-mates-v1/0")
	b.Run(`await wait(()=>document.querySelector('.puzzle-castles button'));const castle=el('.puzzle-castles button');assert(castle.textContent==='Castle e1h1','canonical castle');castle.click();await wait(()=>el('#puzzle-message').textContent.includes('Not the solution move'));assert(!castle.disabled,'castle retry disabled');assert(testBoards.get('board-root').fen()===el('.branch-state').dataset.fen.split(' ')[0],'rejected castle changed board');`)
	b.AssertNoErrors()
}

// Capture only the viewport so Chromium does not resize/hide the fixed tooltip
// while expanding a full-page screenshot of the long collection grid.
func puzzleTooltipScreenshot(t *testing.T, b *browser.Browser, name string) {
	t.Helper()
	b.Run(`await wait(()=>document.getAnimations().every(animation=>animation.playState!=='running'));await frame();assert(!el('#puzzle-tooltip').hidden,'tooltip hidden before capture');`)
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(b.Call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}), &result); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join("..", "..", "docs", "screenshots", name+".png"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
