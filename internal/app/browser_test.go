package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/tests/browser"
)

func browserFixture(t *testing.T, f *httpFixture) *browser.Browser {
	t.Helper()
	if os.Getenv("CHROMIUM_EXECUTABLE_PATH") == "" {
		t.Skip("set CHROMIUM_EXECUTABLE_PATH for production-CSP browser acceptance")
	}
	b := browser.Start(func(req browser.Request) browser.Response {
		r := httptest.NewRequest(req.Method, req.URL, strings.NewReader(req.PostData))
		for k, v := range req.Headers {
			r.Header.Set(k, v)
		}
		if r.Host == "chess.example" {
			f.sign(r, f.owner, true)
		}
		w := response(f.handler, r)
		headers := map[string]string{}
		for k, v := range w.Header() {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		return browser.Response{Body: w.Body.Bytes(), Status: w.Code, Type: w.Header().Get("Content-Type"), Headers: headers}
	})
	t.Cleanup(b.Close)
	b.Call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.cspErrors=[];addEventListener('securitypolicyviolation',e=>cspErrors.push(e.violatedDirective));`})
	return b
}
func screenshot(t *testing.T, b *browser.Browser, name string) {
	t.Helper()
	b.Run(`await wait(()=>jQuery(':animated').length===0);await wait(()=>document.getAnimations().every(animation=>animation.playState!=='running'));await frame();`)
	var result struct {
		Data string `json:"data"`
	}
	if e := json.Unmarshal(b.Call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true}), &result); e != nil {
		t.Fatal(e)
	}
	data, e := base64.StdEncoding.DecodeString(result.Data)
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join("..", "..", "docs", "screenshots")
	if e = os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, name+".png"), data, 0644); e != nil {
		t.Fatal(e)
	}
}

func checkCompactControls(t *testing.T, b *browser.Browser, menu, name string) {
	t.Helper()
	b.Run(`for(const group of document.querySelectorAll('[role=group]')){const input=group.querySelector('input'),button=group.querySelector('button');if(!input||!button)continue;const a=input.getBoundingClientRect(),z=button.getBoundingClientRect();assert(Math.abs(a.top-z.top)<2&&Math.abs(a.bottom-z.bottom)<2,'move group alignment');assert(a.width>=80,'move input too narrow');}const menu=el('#` + menu + `'),trigger=el('[popovertarget="` + menu + `"]');trigger.scrollIntoView({block:'center'});await frame();trigger.click();await frame();const anchor=trigger.getBoundingClientRect(),popup=menu.getBoundingClientRect();assert(Math.abs(popup.left-anchor.left)<2||Math.abs(popup.right-anchor.right)<2,'menu detached horizontally');assert(Math.abs(popup.top-anchor.bottom)<2||Math.abs(popup.bottom-anchor.top)<2,'menu detached vertically');assert(popup.top>=0&&popup.bottom<=innerHeight&&popup.left>=0&&popup.right<=innerWidth,'menu outside viewport '+JSON.stringify({popup,anchor,width:innerWidth,height:innerHeight}));const items=[...menu.querySelectorAll('nav a,nav button')];assert(items.length===11,'missing game actions');const first=items[0].getBoundingClientRect(),color=getComputedStyle(items[1]).color;for(const item of items){assert(getComputedStyle(item).color===color,'menu text colors differ');const box=item.getBoundingClientRect();assert(Math.abs(box.left-first.left)<2&&Math.abs(box.width-first.width)<2,'menu item alignment');assert(box.height>=36,'menu target too small');}items[0].focus();assert(document.activeElement===items[0],'menu keyboard focus');assert(menu.getBoundingClientRect().right<=innerWidth,'menu overflow');`)
	screenshot(t, b, name)
	b.Run(`
const menu=el('#` + menu + `'),trigger=el('[popovertarget="` + menu + `"]');
menu.hidePopover();
const original=trigger.style.cssText;
try {
  for(const bottom of [false,true]) for(const right of [false,true]) {
    trigger.style.cssText='position:fixed;margin:0;'+(bottom?'bottom:16px;':'top:16px;')+(right?'right:16px;':'left:16px;');
    trigger.click();await frame();
    const anchor=trigger.getBoundingClientRect(),popup=menu.getBoundingClientRect();
    assert(Math.abs((right?popup.right:popup.left)-(right?anchor.right:anchor.left))<2,'menu horizontal fallback');
    assert(Math.abs((bottom?popup.bottom:popup.top)-(bottom?anchor.top:anchor.bottom))<2,'menu vertical fallback');
    assert(popup.top>=0&&popup.bottom<=innerHeight&&popup.left>=0&&popup.right<=innerWidth,'fallback outside viewport');
    menu.hidePopover();
  }
  trigger.style.cssText='position:fixed;top:16px;left:50%;margin:0';
  trigger.click();await frame();
  const anchor=trigger.getBoundingClientRect(),popup=menu.getBoundingClientRect();
  assert(Math.abs(popup.top-anchor.bottom)<2,'menu should remain below button');
  assert(popup.left>=0&&popup.right<=innerWidth,'menu horizontal shift');
  menu.hidePopover();
} finally {trigger.style.cssText=original;}
`)
}

func TestBrowserPersistentBoardAndCSP(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	ctx := t.Context()
	created, e := s.Create(ctx, f.owner, uuid.NewString(), store.Setup{Name: "Alexandria — long opponent display name", Color: "w", Mode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	_, session, e := s.Exchange(ctx, created.PlayingToken)
	if e != nil {
		t.Fatal(e)
	}
	b := browserFixture(t, f)
	b.Navigate(f.cfg.Origin + "/matches/" + created.ID)
	b.Run(`await wait(()=>document.querySelectorAll('#match-board img').length===32);assert(window.cspErrors.length===0,JSON.stringify(cspErrors));window.originalBoard=el('#match-board');window.originalDialog=el('#promotion');el('[popovertarget=game-menu]').click();el('input[name=uci]').focus();`)
	// Thirty unchanged polls exercise the production endpoint and HTMX path without waiting thirty seconds.
	b.Run(`for(let i=0;i<30;i++){const state=JSON.parse(el('#match-state').dataset.state);const res=await fetch(el('#match-panel').dataset.base+'/state?'+new URLSearchParams({revision:state.revision,viewers:state.viewers}),{headers:{'HX-Request':'true'}});assert(res.status===204,'unchanged response');}assert(el('#match-board')===originalBoard,'board remounted');assert(el('#promotion')===originalDialog,'dialog remounted');assert(el('#game-menu').matches(':popover-open'),'menu closed');assert(document.activeElement===el('input[name=uci]'),'focus lost');el('#game-menu').hidePopover();el('input[name=uci]').value='e2e4';el('#move-form').requestSubmit();await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===2);assert(el('#match-board')===originalBoard,'move remounted');assert(JSON.parse(el('#match-state').dataset.state).fen.includes(' b '),'move did not apply');`)
	snap, e := s.Snapshot(ctx, created.ID, store.Auth{Session: session})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = s.Mutate(ctx, created.ID, store.Auth{Session: session}, store.Mutation{RequestID: uuid.NewString(), Revision: snap.Revision, Action: "move", Value: "e7e5"})
	if e != nil {
		t.Fatal(e)
	}
	b.Run(`await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===3);assert(el('#match-board')===originalBoard,'remote move remounted');assert(window.cspErrors.length===0,JSON.stringify(cspErrors));`)
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 1000)
			b.Run(`await frame();assert(document.documentElement.scrollWidth<=innerWidth,'horizontal overflow');assert(el('.app-nav').getBoundingClientRect().height<65,'navigation too bulky');const identity=el('.identity');assert(identity.getBoundingClientRect().right<=innerWidth,'identity overflow');const shield=identity.querySelector('svg').getBoundingClientRect();const text=identity.querySelector('strong').getBoundingClientRect();assert(Math.abs((shield.top+shield.height/2)-(text.top+text.height/2))<6,'account alignment');`)
			screenshot(t, b, fmt.Sprintf("owner-%s-%d", theme, width))
			if width == 390 || width == 1440 {
				checkCompactControls(t, b, "game-menu", fmt.Sprintf("menu-%s-%d", theme, width))
			}
		}
	}
	b.Viewport(390, 300)
	b.Navigate(f.cfg.Origin + "/matches/" + created.ID)
	b.Run(`const trigger=el('[popovertarget="game-menu"]');trigger.scrollIntoView({block:'center'});await frame();trigger.click();await frame();const menu=el('#game-menu'),box=menu.getBoundingClientRect();assert(box.top>=0&&box.bottom<=innerHeight&&box.left>=0&&box.right<=innerWidth,'short viewport menu overflow');assert(menu.scrollHeight>menu.clientHeight,'short menu should scroll');menu.querySelector('form button').scrollIntoView();await frame();assert(menu.scrollTop>0,'menu actions unreachable');`)
	b.AssertNoErrors()
}

func TestBrowserGuestBootstrapAndPuzzleBranches(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	ctx := t.Context()
	if e := s.Seed(ctx); e != nil {
		t.Fatal(e)
	}
	created, e := s.Create(ctx, f.owner, uuid.NewString(), store.Setup{Name: "Alexandria — long guest display name", Color: "b", Mode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	b := browserFixture(t, f)
	b.Call("Page.navigate", map[string]any{"url": f.cfg.ShareOrigin + "/g#" + created.PlayingToken})
	b.Run(`await wait(()=>document.readyState==='complete'&&document.querySelector('#guest-continue'));assert(location.hash==='','fragment remains');assert(!document.querySelector('.identity'),'public identity');assert(!document.documentElement.outerHTML.includes(` + browser.Quote(created.PlayingToken) + `),'token rendered');`)
	snap, e := s.Snapshot(ctx, created.ID, store.Auth{Owner: f.owner})
	if e != nil || snap.Status != "pending" {
		t.Fatalf("preview activated: %v", e)
	}
	b.Eval(`document.querySelector("#guest-continue").click()`)
	waitDocument(t, b, `location.pathname.startsWith("/play/")&&document.querySelector("#match-board img")`)
	b.Run(`assert(!document.querySelector('[aria-label="Signed-in account"]'),"guest cloud identity");assert(el('[aria-label="Guest player"] strong').textContent==="Alexandria — long guest display name","guest name");assert(el('[aria-label="Guest player"] .badge').textContent==="Guest","guest label");assert(location.hash==="","guest fragment");`)
	for _, role := range []string{"guest", "viewer"} {
		if role == "viewer" {
			b.Navigate(f.cfg.ShareOrigin + "/watch/" + created.ViewToken)
		}
		for _, theme := range []string{"light", "dark"} {
			b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
			for _, width := range []int{320, 390, 768, 1440} {
				b.Viewport(width, 1000)
				b.Run(`await frame();assert(document.documentElement.scrollWidth<=innerWidth,'public overflow');const guest=document.querySelector('[aria-label=\"Guest player\"]');if(el('#match-panel').dataset.role==='guest'){assert(guest,'missing guest');const label=guest.querySelector('.badge').getBoundingClientRect(),name=guest.querySelector('strong').getBoundingClientRect();assert(name.width>20,'guest name hidden');assert(Math.abs((label.top+label.height/2)-(name.top+name.height/2))<3,'guest label alignment');assert(guest.getBoundingClientRect().right<=innerWidth,'guest header overflow');}else{assert(!guest&&!document.querySelector('[aria-label=\"Signed-in account\"]'),'viewer identity');}assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
				screenshot(t, b, fmt.Sprintf("%s-%s-%d", role, theme, width))
			}
		}
	}
	a, e := s.StartAttempt(ctx, f.owner, "razchess-mates-v1", 9, uuid.NewString(), false)
	if e != nil {
		t.Fatal(e)
	}
	b.Navigate(f.cfg.Origin + "/puzzle-attempts/" + a.ID)
	def, e := s.Definition(ctx, "razchess-mates-v1", 9)
	if e != nil {
		t.Fatal(e)
	}
	b.Run(`await wait(()=>document.querySelector('.branch .board img'));window.rootBoard=el('.branch .board');const form=el('.puzzle-move');form.elements.uci.value=` + browser.Quote(def.Root.Move) + `;form.requestSubmit();await wait(()=>document.querySelectorAll('.branch').length===3);assert(rootBoard.isConnected,'root removed');window.childBoard=document.querySelectorAll('.branch .board')[2];window.childForm=document.querySelectorAll('.puzzle-move')[0];`)
	for _, theme := range []string{"light", "dark"} {
		b.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
		for _, width := range []int{320, 390, 768, 1440} {
			b.Viewport(width, 1000)
			b.Run(`await frame();assert(document.documentElement.scrollWidth<=innerWidth,'puzzle overflow');assert(cspErrors.length===0,JSON.stringify(cspErrors));const summary=el('.branch[data-parent=true]').getBoundingClientRect();const child=el('.branch[data-parent=false]').getBoundingClientRect();assert(summary.bottom<=child.top,'retired parent did not compact above live boards');`)
			screenshot(t, b, fmt.Sprintf("puzzle-%s-%d", theme, width))
			if width == 390 || width == 1440 {
				checkCompactControls(t, b, "puzzle-menu", fmt.Sprintf("puzzle-menu-%s-%d", theme, width))
			}
		}
	}
	// Acknowledged source paths advance only the selected child; its sibling DOM survives.
	move := def.Root.Replies[0].Next.Move
	b.Run(`childForm.elements.uci.value=` + browser.Quote(move) + `;childForm.requestSubmit();await wait(()=>Number(el('#attempt-state').dataset.revision)>=2);assert(childBoard.isConnected,'sibling board replaced');`)
	b.AssertNoErrors()
	// Editor preserves all FEN counters and readonly export URLs require the selected branch.
	fen := "7k/8/5KQ1/8/8/8/8/8 w - - 17 23"
	b.Navigate(f.cfg.Origin + "/create?" + url.Values{"fen": {fen}}.Encode())
	b.Run(`assert(el('#edit-fen').value===` + browser.Quote(fen) + `,'FEN counters lost');assert(el('#edit-half').value==='17'&&el('#edit-full').value==='23','counter controls');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	b.AssertNoErrors()
}

func waitDocument(t *testing.T, b *browser.Browser, condition string) {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		ok := false
		func() { defer func() { _ = recover() }(); ok = string(b.Eval("!!("+condition+")")) == "true" }()
		if ok {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("document did not become ready: " + condition)
}

func TestBrowserNavigationDraftAndStaleState(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	created, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Navigation fixture", Color: "w", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Exchange(t.Context(), created.PlayingToken); err != nil {
		t.Fatal(err)
	}
	b := browserFixture(t, f)
	b.Navigate(f.cfg.Origin + "/matches/" + created.ID)
	b.Run(`window.header=el('.cloud-header');window.initialBoard=el('#match-board');window.oldState=await(await fetch(el('#match-panel').dataset.base+'/state')).text();el('#move-form').elements.uci.value='e2e4';el('#move-form').requestSubmit();await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===2);document.dispatchEvent(new CustomEvent('htmx:beforeSwap',{detail:{target:el('#match-state'),xhr:{status:200,responseText:oldState}}}));assert(JSON.parse(el('#match-state').dataset.state).revision===2,'delayed state rewound move');el('.app-nav a[href="/matches/archive"]').click();await wait(()=>location.pathname==='/matches/archive'&&document.querySelector('h1')?.textContent==='Archive');assert(el('.cloud-header')===header,'shell replaced');assert(el('.app-nav [aria-current=page]').pathname==='/matches/archive','navigation marker stale');assert(!initialBoard.isConnected,'board retained after navigation');el('.app-nav a[href="/matches/new"]').click();await wait(()=>document.querySelector('#editor-board img'));assert(el('.cloud-header')===header,'new-page shell replaced');el('#edit-fen').value='7k/8/5KQ1/8/8/8/8/8 w - - 17 23';el('#edit-fen').dispatchEvent(new Event('input',{bubbles:true}));el('.app-nav a[href="/matches"]').click();await frame();`)
	if b.DialogCount() != 1 {
		t.Fatalf("draft navigation did not confirm: %d", b.DialogCount())
	}
	b.Run(`assert(document.querySelector('#setup'),'draft discarded');assert(el('#edit-fen').value.includes('17 23'),'draft changed');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	b.AssertNoErrors()
}

func drag(b *browser.Browser, from, to string) {
	b.Run(`await wait(()=>jQuery(':animated').length===0);await frame();`)
	var points struct{ X1, Y1, X2, Y2 float64 }
	_ = json.Unmarshal(b.Eval(`(()=>{const a=document.querySelector('#match-board [data-square="`+from+`"]').getBoundingClientRect(),b=document.querySelector('#match-board [data-square="`+to+`"]').getBoundingClientRect();return {X1:a.x+a.width/2,Y1:a.y+a.height/2,X2:b.x+b.width/2,Y2:b.y+b.height/2}})()`), &points)
	b.Call("Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": points.X1, "y": points.Y1, "button": "left", "buttons": 1, "clickCount": 1})
	b.Call("Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": points.X2, "y": points.Y2, "button": "left", "buttons": 1})
	b.Call("Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": points.X2, "y": points.Y2, "button": "left", "buttons": 0, "clickCount": 1})
}

func TestBrowserPromotionAndAccessibleCastle(t *testing.T) {
	s := httpStore(t)
	f := newHTTPFixture(t, s)
	created, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Promotion", Color: "w", Mode: "custom", FEN: "7k/P7/8/8/8/8/8/7K w - - 0 1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Exchange(t.Context(), created.PlayingToken); err != nil {
		t.Fatal(err)
	}
	b := browserFixture(t, f)
	b.Navigate(f.cfg.Origin + "/matches/" + created.ID)
	b.Run(`await wait(()=>document.querySelector('#match-board [data-square=a7] img'));window.dialog=el('#promotion');`)
	drag(b, "a7", "a8")
	b.Run(`await wait(()=>el('#promotion').open);`)
	b.Escape()
	b.Run(`assert(!dialog.open,'promotion cancel');assert(JSON.parse(el('#match-state').dataset.state).revision===1,'cancel sent move');`)
	drag(b, "a7", "a8")
	b.Run(`await wait(()=>dialog.open);el('[data-promotion=n]').click();await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===2);assert(el('#promotion')===dialog,'promotion dialog replaced');assert(JSON.parse(el('#match-state').dataset.state).fen.startsWith('N6k/'),'underpromotion lost');`)
	castle, err := s.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Castling", Color: "w", Mode: "custom", FEN: "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Exchange(t.Context(), castle.PlayingToken); err != nil {
		t.Fatal(err)
	}
	b.Navigate(f.cfg.Origin + "/matches/" + castle.ID)
	b.Run(`await wait(()=>el('#castle-controls').querySelectorAll('button').length===2);const castleButton=[...el('#castle-controls').querySelectorAll('button')].find(b=>b.textContent.includes('e1h1'));castleButton.click();await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===2);assert(JSON.parse(el('#match-state').dataset.state).fen.includes('R4RK1'),'castle pieces not reconciled');assert(cspErrors.length===0,JSON.stringify(cspErrors));`)
	b.AssertNoErrors()
}
