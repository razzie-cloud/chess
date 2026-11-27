package app

import (
	"fmt"
	"testing"

	"github.com/razzie-cloud/chess/tests/browser"
)

func checkPromotionDialog(t *testing.T, b *browser.Browser, id, color string) {
	t.Helper()
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 900)
			b.Run(`await frame();const dialog=el('#` + id + `'),box=dialog.getBoundingClientRect();assert(dialog.open,'promotion closed on resize');assert(box.width<=352&&box.height<250,'promotion fills screen');assert(box.left>=0&&box.right<=innerWidth&&box.top>=0&&box.bottom<=innerHeight,'promotion overflow');assert(Math.abs(box.left+box.width/2-document.documentElement.clientWidth/2)<2&&Math.abs(box.top+box.height/2-innerHeight/2)<2,'promotion not centered '+JSON.stringify({box:box.toJSON(),width:document.documentElement.clientWidth,height:innerHeight}));assert(el('#'+dialog.getAttribute('aria-labelledby')).textContent==='Choose promotion','dialog unnamed');const names=['Queen','Rook','Bishop','Knight'],pieces=['Q','R','B','N'],buttons=[...dialog.querySelectorAll('.promotion-choices button')];assert(buttons.length===4,'missing choices');for(let i=0;i<4;i++){const button=buttons[i],image=button.querySelector('img');await wait(()=>image.complete&&image.naturalWidth>0);assert(button.textContent.trim()===''&&button.getAttribute('aria-label')===names[i],'piece label');assert(image.getAttribute('src')==='/static/pieces/` + color + `'+pieces[i]+'.png','wrong piece color');const target=button.getBoundingClientRect();assert(target.width>=44&&target.height>=44,'piece target too small');button.focus();assert(document.activeElement===button,'piece cannot receive keyboard focus');}assert(document.documentElement.scrollWidth<=innerWidth,'page overflow');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
			if (theme == "light" && width == 1440) || (theme == "dark" && width == 390) {
				screenshot(t, b, fmt.Sprintf("%s-%s-%s-%d", id, color, theme, width))
			}
		}
	}
}

func TestBrowserPuzzlePromotionDialog(t *testing.T) {
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	b := browserFixture(t, f)
	for _, tc := range []struct{ slot, color, from, to string }{
		{"1", "w", "c7", "b8"},
		{"29", "b", "g2", "g1"},
	} {
		b.Navigate(f.cfg.Origin + "/puzzles/razchess-mates-v1/" + tc.slot)
		b.Run(`await wait(()=>document.querySelector('#board-root img'));`)
		dragBoard(b, "#board-root", tc.from, tc.to)
		b.Run(`await wait(()=>el('#puzzle-promotion').open);`)
		checkPromotionDialog(t, b, "puzzle-promotion", tc.color)
		b.Escape()
		b.Run(`assert(!el('#puzzle-promotion').open,'Escape kept promotion open');assert(el('#puzzle-state').dataset.revision==='0','cancel sent a move');`)
		dragBoard(b, "#board-root", tc.from, tc.to)
		b.Run(`await wait(()=>el('#puzzle-promotion').open);el('[data-piece=cancel]').click();await frame();assert(!el('#puzzle-promotion').open,'Cancel kept promotion open');assert(el('#puzzle-state').dataset.revision==='0','cancel sent a move');`)
		dragBoard(b, "#board-root", tc.from, tc.to)
		b.Run(`await wait(()=>el('#puzzle-promotion').open);el('[data-piece=n] img').click();await wait(()=>el('#puzzle-message').textContent.includes('Not the solution move'));assert(!el('#puzzle-promotion').open,'selection kept promotion open');assert(el('#puzzle-state').dataset.revision==='0','wrong promotion advanced puzzle');`)
	}
	b.AssertNoErrors()
}
