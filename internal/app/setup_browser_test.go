package app

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/tests/browser"
)

func TestBrowserMatchCreators(t *testing.T) {
	f := newHTTPFixture(t, httpStore(t))
	b := browserFixture(t, f)
	for _, variant := range []struct{ path, mode, board string }{
		{"/matches/new", "standard", "#setup-board"},
		{"/matches/new?mode=chess960", "chess960", "#setup-board"},
		{"/create", "custom", "#editor-board"},
	} {
		if variant.mode == "chess960" {
			b.Run(`window.creatorHeader=el('.cloud-header');el('[popovertarget=new-game-menu]').click();await frame();el('#new-game-menu a[href="/matches/new?mode=chess960"]').click();await wait(()=>document.querySelector('#refresh-chess960')&&document.querySelector('#setup-board img'));const before=el('[name=chess960_index]').value;el('#refresh-chess960').click();await wait(()=>el('[name=chess960_index]').value!==before);assert(el('.cloud-header')===creatorHeader,'refresh after navigation replaced shell');el('#setup').dispatchEvent(new Event('submit'));`)
		}
		for _, theme := range []string{"light", "dark"} {
			b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
			for _, width := range []int{320, 390, 768, 1440} {
				b.Viewport(width, 1000)
				b.Navigate(f.cfg.Origin + variant.path)
				b.AssertNoErrors()
				b.Run(`await wait(()=>document.querySelectorAll('` + variant.board + ` img').length>=32);await frame();assert(document.querySelectorAll('h1').length===1,'heading');assert(el('#mode').value===` + browser.Quote(variant.mode) + `,'mode');assert(document.documentElement.scrollWidth<=document.documentElement.clientWidth,'setup overflow');assert([...document.images].every(i=>i.complete&&i.naturalWidth),'unloaded image');assert(visible(el('.setup-create')),'create action');if(innerWidth>850){const a=el('.setup-position').getBoundingClientRect(),z=el('.setup-sidebar').getBoundingClientRect();assert(a.right<z.left&&Math.abs(a.top-z.top)<2,'desktop columns');}el('#setup input[name=color][value=b]').click();await wait(()=>jQuery(':animated').length===0);await frame();const a=el('` + variant.board + ` [data-square=a8]').getBoundingClientRect(),z=el('` + variant.board + ` [data-square=h1]').getBoundingClientRect();assert(a.left>z.left&&a.top>z.top,'color preview '+JSON.stringify({a:a.toJSON(),z:z.toJSON(),color:el('#setup input[name=color]:checked').value,errors:cspErrors}));await click('Flip board');`)
				if variant.mode == "custom" {
					b.Run(`assert(!el('.setup-rules').open,'advanced rules expanded');el('input[name=source][value=fen]').click();assert(visible(el('#fen-options'))&&!visible(el('#board-options')),'FEN panel');assert(!visible(el('.setup-board-tools')),'board tools in FEN source');el('#edit-fen').value='draft FEN';el('input[name=source][value=pgn]').click();el('#edit-pgn').value='draft PGN';el('input[name=source][value=board]').click();assert(el('#edit-fen').value==='draft FEN'&&el('#edit-pgn').value==='draft PGN','source draft lost');el('.setup-rules').open=true;assert(visible(el('#edit-full')),'advanced fields');await click('Clear board');assert(el('#edit-fen').value==='8/8/8/8/8/8/8/8 w - - 0 1','clear rules');await click('Default position');assert(el('#edit-fen').value===` + browser.Quote(game.StartFEN) + `,'default rules');el('.setup-rules').open=false;el('input[name=source][value=board]').focus();`)
					for _, typ := range []string{"keyDown", "keyUp"} {
						b.Call("Input.dispatchKeyEvent", map[string]any{"type": typ, "key": "ArrowRight", "code": "ArrowRight", "windowsVirtualKeyCode": 39})
					}
					b.Run(`assert(el('input[name=source][value=fen]').checked&&visible(el('#fen-options')),'keyboard source choice');el('input[name=source][value=board]').click();el('#edit-pgn').value='';`)
				} else {
					b.Run(`assert(!document.querySelector('#editor-board,#edit-pgn,[name=fen]'),'simple creator includes editor');`)
					if variant.mode == "chess960" {
						b.Run(`
const before=el('[name=chess960_index]').value,boardNode=el('#setup-board'),form=el('#setup'),name=form.elements.name,request=form.elements.request_id.value,header=el('.cloud-header'),url=location.href,historyLength=history.length;
// Reproduce browsers that blur a focused button when HTMX disables it.
el('#refresh-chess960').addEventListener('htmx:beforeSend',event=>event.currentTarget.blur(),{once:true});
name.value='Draft opponent';name.dispatchEvent(new Event('input',{bubbles:true}));const refresh=el('#refresh-chess960');refresh.focus();refresh.click();assert(refresh.disabled&&el('.setup-create').disabled,'refresh did not block competing creation');
await wait(()=>el('[name=chess960_index]').value!==before&&!refresh.disabled&&el('#setup-board').dataset.fen===el('#chess960-position').dataset.fen);
assert(document.activeElement===refresh,'refresh lost keyboard focus');assert(boardNode===el('#setup-board')&&form===el('#setup')&&name===form.elements.name&&header===el('.cloud-header'),'refresh replaced stable nodes');assert(name.value==='Draft opponent'&&form.elements.request_id.value===request,'refresh lost form draft');assert(el('#setup input[name=color][value=b]').checked,'refresh lost color');assert(location.href===url&&history.length===historyLength,'refresh changed history');
const a=el('#setup-board [data-square=a8]').getBoundingClientRect(),z=el('#setup-board [data-square=h1]').getBoundingClientRect();assert(a.left<z.left&&a.top<z.top,'refresh lost flipped orientation');assert(el('.setup-position .setup-hint').textContent.includes('will be used'),'selected position not explained');
const resource=performance.getEntriesByType('resource').filter(r=>r.name.includes('/matches/new/chess960')).at(-1);assert(resource,'missing HTMX GET');const params=new URL(resource.name).searchParams;assert(params.get('chess960_index')===before&&[...params.keys()].every(k=>k==='chess960_index'),'refresh sent the wrong position or form details');name.value='';
`)
					}
				}
				if variant.mode == "chess960" && width == 1440 && theme == "dark" {
					b.Call("Network.enable", nil)
					b.Call("Network.setBlockedURLs", map[string]any{"urls": []string{"*/matches/new/chess960*"}})
					b.Run(`window.keptIndex=el('[name=chess960_index]').value;window.keptFEN=el('#setup-board').dataset.fen;const refresh=el('#refresh-chess960');refresh.addEventListener('htmx:beforeSend',()=>refresh.blur(),{once:true});refresh.focus();refresh.click();await wait(()=>el('#chess960-feedback').textContent.includes('Unable to refresh')&&!refresh.disabled);assert(document.activeElement===refresh,'failed refresh lost keyboard focus');assert(el('[name=chess960_index]').value===keptIndex&&el('#setup-board').dataset.fen===keptFEN,'failed refresh changed selection');assert(!el('.setup-create').disabled,'failed refresh left creation disabled');`)
					b.Call("Network.setBlockedURLs", map[string]any{"urls": []string{}})
					b.Run(`const refresh=el('#refresh-chess960'),name=el('#setup').elements.name;refresh.focus();refresh.click();name.focus();await wait(()=>el('[name=chess960_index]').value!==keptIndex&&!refresh.disabled);assert(document.activeElement===name,'refresh stole focus from another field');assert(el('#chess960-feedback').textContent==='New starting position ready.','retry failed');`)
				}
				b.Run(`assert(cspErrors.length===0,JSON.stringify(cspErrors));document.activeElement?.blur();`)
				if width == 390 || width == 1440 {
					screenshot(t, b, fmt.Sprintf("setup-%s-%s-%d", variant.mode, theme, width))
				}
				// These layout-only edits are complete; let the fixture navigate without dismissing a draft warning.
				b.Run(`el('#setup').dispatchEvent(new Event('submit'));`)
			}
		}
		if variant.mode == "custom" {
			b.Run(`el('input[name=source][value=fen]').click();el('#edit-fen').value='7k/8/5KQ1/8/8/8/8/8 w - - invalid 23';el('#edit-fen').dispatchEvent(new Event('change',{bubbles:true}));el('#edit-pgn').value='inactive PGN draft';el('#setup').elements.name.value='Alex';el('button[formaction]').click();`)
			waitDocument(t, b, `document.querySelector('[data-error-summary]')`)
			b.Run(`assert(el('#edit-fen').value==='7k/8/5KQ1/8/8/8/8/8 w - - invalid 23'&&el('#edit-pgn').value==='inactive PGN draft','validation lost drafts');assert(el('input[name=source][value=fen]').checked,'validation lost source');assert(el('#setup').elements.name.value==='Alex','validation lost name');assert(el('input[name=color][value=b]').checked,'validation lost color');el('#edit-fen').value=` + browser.Quote(game.StartFEN) + `;el('button[formaction]').click();`)
			waitDocument(t, b, `document.querySelector('.notice')&&!document.querySelector('[data-error-summary]')`)
			b.Run(`assert(el('#edit-pgn').value==='inactive PGN draft','success lost PGN draft');assert(el('#edit-fen').value===` + browser.Quote(game.StartFEN) + `,'wrong validation source');el('input[name=source][value=pgn]').click();el('#edit-pgn').value='1. e4 e5 *';el('button[formaction]').click();`)
			waitDocument(t, b, `document.querySelector('#editor-board [data-square=e5] img')`)
			b.Run(`assert(el('input[name=source][value=pgn]').checked,'PGN source lost');assert(el('#edit-pgn').value==='1. e4 e5 *','PGN draft lost');assert(el('#edit-fen').value.includes('4p3/4P3'),'PGN not replayed');el('input[name=source][value=fen]').click();el('#edit-pgn').value='inactive invalid PGN';el('#setup').elements.name.value=' invalid name ';el('#setup').requestSubmit();`)
			waitDocument(t, b, `document.querySelector('[data-error-summary]')`)
			b.Run(`assert(document.querySelector('#editor-board'),'create error lost editor');assert(el('input[name=source][value=fen]').checked,'create error lost source');assert(el('#edit-pgn').value==='inactive invalid PGN','create error lost draft');assert(el('#setup').elements.name.value===' invalid name ','create error lost name');`)
		}
		var selected960 string
		if variant.mode == "chess960" {
			if err := json.Unmarshal(b.Eval(`document.querySelector('#setup-board').dataset.fen`), &selected960); err != nil {
				t.Fatal(err)
			}
			initial, err := game.New(selected960, true)
			if err != nil {
				t.Fatal(err)
			}
			selected960 = initial.InitialFEN
		}
		b.Run(`el('#setup').elements.name.value='Creator acceptance';el('#setup').requestSubmit();`)
		waitDocument(t, b, `document.querySelector('#match-board img')&&document.querySelector('[data-playing-link]')`)
		b.Run(`assert(JSON.parse(el('#match-state').dataset.state).color==='b','creator color lost');assert(el('[data-playing-link]').value.startsWith('https://shared-chess.example/g#'),'playing link missing');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
		if variant.mode == "standard" {
			b.Run(`assert(JSON.parse(el('#match-state').dataset.state).fen===` + browser.Quote(game.StartFEN) + `,'normal start changed');`)
		} else if variant.mode == "chess960" {
			b.Run(`assert(JSON.parse(el('#match-state').dataset.state).fen===` + browser.Quote(selected960) + `,'created match differs from refreshed board');`)
		} else if variant.mode == "custom" {
			b.Run(`assert(JSON.parse(el('#match-state').dataset.state).fen.includes('4p3/4P3'),'wrong source created match');`)
		}
	}
	b.AssertNoErrors()
}
