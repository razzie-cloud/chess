package app

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/tests/browser"
)

// Dispatch real touch input so the browser can exercise its default scrolling.
func touchDrag(b *browser.Browser, from, to string) {
	b.Run(`el(` + browser.Quote(from) + `).scrollIntoView({block:'center'});await wait(()=>jQuery(':animated').length===0);await frame();window.touchScrollY=scrollY;`)
	var points struct{ X1, Y1, X2, Y2 float64 }
	if err := json.Unmarshal(b.Eval(`(()=>{const a=document.querySelector(`+browser.Quote(from)+`).getBoundingClientRect(),z=document.querySelector(`+browser.Quote(to)+`).getBoundingClientRect();return {X1:a.x+a.width/2,Y1:a.y+a.height/2,X2:z.x+z.width/2,Y2:z.y+z.height/2}})()`), &points); err != nil {
		panic(err)
	}
	touchSwipe(b, points.X1, points.Y1, points.X2, points.Y2)
	b.Run(`assert(Math.abs(scrollY-touchScrollY)<1,'piece drag scrolled the page: '+touchScrollY+' -> '+scrollY);`)
	b.Call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []any{}})
}

func touchSwipe(b *browser.Browser, x1, y1, x2, y2 float64) {
	touch := func(kind string, x, y float64) {
		b.Call("Input.dispatchTouchEvent", map[string]any{"type": kind, "touchPoints": []map[string]any{{"x": x, "y": y, "id": 1}}})
		b.Run(`await frame();`)
	}
	touch("touchStart", x1, y1)
	for i := 1; i <= 10; i++ {
		part := float64(i) / 10
		touch("touchMove", x1+(x2-x1)*part, y1+(y2-y1)*part)
	}
}

func TestBrowserMobileTouchDragging(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Touch player", Color: "w", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	b := browserFixture(t, f)
	b.Call("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": true, "maxTouchPoints": 1})
	b.Call("Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 700, "deviceScaleFactor": 1, "mobile": true})
	// Bootstrap the guest, then exercise both players on the same match.
	b.Call("Page.navigate", map[string]any{"url": f.cfg.ShareOrigin + "/g#" + created.PlayingToken})
	b.Run(`await wait(()=>document.readyState==='complete'&&document.querySelector('#guest-continue'));el('#guest-continue').click();`)
	waitDocument(t, b, `document.querySelector('#match-board img')`)
	var guest string
	if err := json.Unmarshal(b.Eval(`location.href`), &guest); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		b.Navigate(f.cfg.Origin + "/matches/" + created.ID)
		from, to := "e2", "e4"
		if theme == "dark" {
			from, to = "d2", "d4"
		}
		touchDrag(b, "#match-board [data-square="+from+"]", "#match-board [data-square="+to+"]")
		b.Run(`await wait(()=>document.querySelector('#match-board [data-square=` + to + `] img'));`)
		b.Navigate(guest)
		if theme == "light" {
			from, to = "e7", "e5"
		} else {
			from, to = "d7", "d5"
		}
		touchDrag(b, "#match-board [data-square="+from+"]", "#match-board [data-square="+to+"]")
		b.Run(`await wait(()=>document.querySelector('#match-board [data-square=` + to + `] img'));`)

		b.Navigate(f.cfg.Origin + "/puzzles/razchess-mates-v1/9")
		def, err := s.Definition(t.Context(), "razchess-mates-v1", 9)
		if err != nil {
			t.Fatal(err)
		}
		move := def.Root.Move
		touchDrag(b, "#board-root [data-square="+move[:2]+"]", "#board-root [data-square="+move[2:4]+"]")
		b.Run(`await wait(()=>document.querySelectorAll('.branch').length===2);`)
		move = def.Root.Replies[0].Next.Move
		touchDrag(b, "#board-root-1 [data-square="+move[:2]+"]", "#board-root-1 [data-square="+move[2:4]+"]")
		b.Run(`await wait(()=>el('#puzzle-state').dataset.revision==='2');`)

		b.Navigate(f.cfg.Origin + "/create")
		touchDrag(b, "#editor-board [data-square=e2]", "#editor-board [data-square=e4]")
		b.Run(`await wait(()=>document.querySelector('#editor-board [data-square=e4] img'));assert(!document.querySelector('#editor-board [data-square=e2] img'),'editor touch move lost');`)
		touchDrag(b, "#editor-board .spare-pieces-bottom-ae20f [data-piece=wQ]", "#editor-board [data-square=e3]")
		b.Run(`await wait(()=>document.querySelector('#editor-board [data-square=e3] img'));assert(el('#editor-board [data-square=e3] img').dataset.piece==='wQ','spare piece touch lost');el('#setup').dispatchEvent(new Event('submit'));assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
		b.Run(`window.outsideScrollY=scrollY;`)
		touchSwipe(b, 8, 500, 8, 350)
		b.Call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []any{}})
		b.Run(`assert(scrollY>outsideScrollY,'touch scrolling outside the board was blocked');`)
	}
	b.Navigate(f.cfg.ShareOrigin + "/watch/" + created.ViewToken)
	b.Run(`window.viewerScrollY=scrollY;`)
	touchSwipe(b, 195, 450, 195, 300)
	b.Call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []any{}})
	b.Run(`assert(scrollY>viewerScrollY,'read-only board blocked touch scrolling');`)
	b.AssertNoErrors()
}
