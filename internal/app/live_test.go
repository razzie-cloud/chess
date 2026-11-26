package app

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/razzie-cloud/chess/internal/db"
	"github.com/razzie-cloud/chess/internal/puzzle"
	"github.com/razzie-cloud/chess/tests/browser"
)

// This fixture deliberately uses real network traffic and Gateway-issued
// assertions. scripts/live-acceptance.sh owns its disposable backend lifecycle.
func TestLiveGatewaySSOAndAnonymousGuest(t *testing.T) {
	origin, share, auth := os.Getenv("CHESS_LIVE_ORIGIN"), os.Getenv("CHESS_LIVE_SHARE_ORIGIN"), os.Getenv("CHESS_LIVE_AUTH_ORIGIN")
	if origin == "" || share == "" || auth == "" || os.Getenv("CHROMIUM_EXECUTABLE_PATH") == "" {
		t.Skip("run scripts/live-acceptance.sh with local Chromium for live Gateway/Caddy acceptance")
	}
	for _, value := range []string{origin, share, auth} {
		if !strings.HasPrefix(value, "http://") || !strings.Contains(value, ".localhost:") {
			t.Fatal("live acceptance accepts only disposable .localhost origins")
		}
	}
	logFile, err := os.Open(os.Getenv("CHESS_LIVE_GATEWAY_LOG"))
	if err != nil {
		t.Fatal("cannot read private fixture bootstrap log")
	}
	defer logFile.Close()
	token := ""
	scanner := bufio.NewScanner(logFile)
	for scanner.Scan() {
		var event struct {
			Event string `json:"event"`
			Token string `json:"bootstrap_token"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Event == "bootstrap.token_created" {
			token = event.Token
		}
	}
	if scanner.Err() != nil || len(token) != 64 {
		t.Fatal("fixture has no bootstrap credential")
	}
	owner := browser.Start(nil)
	t.Cleanup(owner.Close)
	owner.Call("WebAuthn.enable", nil)
	owner.Call("WebAuthn.addVirtualAuthenticator", map[string]any{"options": map[string]any{
		"protocol": "ctap2", "transport": "internal", "hasResidentKey": true,
		"hasUserVerification": true, "isUserVerified": true, "automaticPresenceSimulation": true,
	}})
	var navigation struct {
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(owner.Call("Page.navigate", map[string]any{"url": auth + "/"}), &navigation); err != nil || navigation.ErrorText != "" {
		t.Fatalf("live Gateway navigation failed: %s", navigation.ErrorText)
	}
	liveWait(t, owner, `document.readyState==='complete'&&document.querySelector('input[name=token]')!==null`)
	// Do not pass the credential through Eval/Run: their assertion diagnostics
	// include source expressions. Keep it out of every success/failure message.
	result := owner.Call("Runtime.evaluate", map[string]any{"expression": `document.querySelector('input[name=token]').value=` + browser.Quote(token)})
	var setResult struct {
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(result, &setResult) != nil || len(setResult.Exception) != 0 {
		t.Fatal("unable to fill fixture bootstrap credential")
	}
	token = ""
	owner.Run(`el('input[name=username]').value='chess-live-owner';el('input[name=deviceName]').value='Disposable virtual passkey';document.querySelector('form[data-webauthn=register]').requestSubmit();await wait(()=>document.querySelector('.identity strong')?.textContent==='chess-live-owner');`)
	owner.Navigate(origin + "/matches/new")
	owner.Run(`assert(el('.identity strong').textContent==='chess-live-owner','SSO identity missing');assert(el('.identity .badge').textContent==='Administrator','signed admin role missing');`)
	owner.Eval(`document.querySelector('input[name=name]').value='Anonymous live guest';document.querySelector('#setup').requestSubmit();`)
	liveWait(t, owner, `document.querySelector('#match-panel')&&document.querySelector('[data-playing-link]')`)
	var playingURL string
	if err := json.Unmarshal(owner.Eval(`document.querySelector('[data-playing-link]').value`), &playingURL); err != nil {
		t.Fatal("cannot read fixture playing link")
	}
	var matchID string
	if err := json.Unmarshal(owner.Eval(`document.querySelector('#match-panel').dataset.resource`), &matchID); err != nil {
		t.Fatal(err)
	}
	owner.Navigate(origin + "/matches/" + matchID)
	guest := browser.Start(nil)
	t.Cleanup(guest.Close)
	guest.Call("Page.navigate", map[string]any{"url": playingURL})
	playingURL = ""
	liveWait(t, guest, `document.readyState==='complete'&&document.querySelector('#guest-continue')`)
	guest.Run(`assert(location.hash==='','playing fragment not scrubbed');assert(!document.querySelector('.identity'),'guest got cloud identity');`)
	owner.Run(`const response=await fetch(el('#match-panel').dataset.base+'/state');const doc=new DOMParser().parseFromString(await response.text(),'text/html');assert(JSON.parse(doc.querySelector('#match-state').dataset.state).status==='pending','preview activated match');`)
	guest.Eval(`document.querySelector('#guest-continue').click()`)
	liveWait(t, guest, `location.pathname.startsWith('/play/')&&document.querySelector('#match-board img')`)
	guest.Run(`assert(!document.querySelector('[aria-label="Signed-in account"]'),'guest has cloud registration');assert(el('[aria-label="Guest player"] .badge').textContent==='Guest','guest badge missing');assert(el('h1').textContent==='chess-live-owner vs Anonymous live guest','guest opponent title');assert((await fetch('/puzzles')).status===404,'public proxy exposed personal puzzles');`)
	owner.Run(`await wait(()=>JSON.parse(el('#match-state').dataset.state).status==='active');el('input[name=uci]').value='e2e4';el('#move-form').requestSubmit();await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===2);`)
	restartLiveReplica(t)
	guest.Run(`await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===2);el('input[name=uci]').value='e7e5';el('#move-form').requestSubmit();await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===3);const response=await fetch(el('#match-panel').dataset.base+'/export/pgn');assert(response.ok,'guest export denied');assert((await response.text()).includes('1. e4 e5'),'guest export lost played history');`)
	owner.Run(`await wait(()=>JSON.parse(el('#match-state').dataset.state).revision===3);assert(JSON.parse(el('#match-state').dataset.state).history.length===2,'creator missed remote move');`)
	var bundle puzzle.Bundle
	if err := json.Unmarshal(db.PuzzleSeeds(), &bundle); err != nil {
		t.Fatal(err)
	}
	var definition puzzle.Definition
	for _, collection := range bundle.Collections {
		if collection.ID == "razchess-mates-v1" {
			for _, candidate := range collection.Puzzles {
				if candidate.Slot == 9 {
					definition = candidate
				}
			}
		}
	}
	if definition.Root == nil || definition.Forks != 1 {
		t.Fatal("live fixture requires its reviewed branching seed")
	}
	owner.Navigate(origin + "/puzzles/razchess-mates-v1/9")
	liveWait(t, owner, `document.querySelector('.puzzle-move')`)
	owner.Run(`const form=el('.puzzle-move');form.elements.uci.value=` + browser.Quote(definition.Root.Move) + `;form.requestSubmit();await wait(()=>Number(el('#puzzle-state').dataset.revision)===1);assert(document.querySelectorAll('.branch').length===2,'compiled fork not expanded');`)
	restartLiveReplica(t)
	var puzzleURL string
	if err := json.Unmarshal(owner.Eval(`location.href`), &puzzleURL); err != nil {
		t.Fatal(err)
	}
	// The client retains its full state while replicas restart.
	owner.Run(`assert(JSON.parse(el('#puzzle-state').dataset.state).boards.length===2,'client lost branching state');`)
	for decision := 0; decision < definition.Decisions; decision++ {
		var branches []struct {
			ID   string   `json:"id"`
			Path []string `json:"path"`
		}
		raw := owner.Eval(`[...document.querySelectorAll('.branch-state[data-done=false]')].map(node=>({id:node.dataset.branch,path:JSON.parse(node.dataset.path)}))`)
		if err := json.Unmarshal(raw, &branches); err != nil {
			t.Fatal(err)
		}
		if len(branches) == 0 {
			break
		}
		node, err := puzzle.Lookup(definition, branches[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		owner.Run(`const branch=[...document.querySelectorAll('.branch')].find(node=>node.dataset.branch===` + browser.Quote(branches[0].ID) + `);const revision=Number(el('#puzzle-state').dataset.revision);const form=branch.querySelector('form');form.elements.uci.value=` + browser.Quote(node.Move) + `;form.requestSubmit();await wait(()=>Number(el('#puzzle-state').dataset.revision)===revision+1);`)
	}
	owner.Run(`assert(el('#puzzle-state').dataset.done==='true','not every branch completed');`)
	owner.Navigate(origin + "/puzzles/razchess-mates-v1")
	owner.Run(`assert(document.querySelector('.puzzle-grid a.solved[href$="/9"]'),'personal completion bit missing after restart');`)
	// Both pages use the same actual Gateway session, so its real sign-out must
	// expire their access while leaving the independent guest capability valid.
	puzzleBrowser := liveSessionBrowser(t, owner, origin, auth)
	puzzleBrowser.Navigate(puzzleURL)
	owner.Navigate(origin + "/matches/" + matchID)
	signoutBrowser := liveSessionBrowser(t, owner, origin, auth)
	signoutBrowser.Navigate(auth + "/")
	signoutBrowser.Run(`el('form[hx-post="/ui/logout"]').requestSubmit();await wait(()=>!document.querySelector('.identity strong'));`)
	liveWait(t, owner, `document.querySelector('#connection [data-sign-in]')`)
	owner.Run(`assert(el('#move-form button[type=submit]').disabled,'expired match still accepts moves');assert(!document.querySelector('form[data-webauthn]'),'login page replaced board');assert(el('#connection [data-sign-in]').getAttribute('href').startsWith('/matches/'),'sign-in link is not native same-origin');`)
	puzzleBrowser.Run(`const form=el('.puzzle-move');form.elements.uci.value=` + browser.Quote(definition.Root.Move) + `;form.requestSubmit();await wait(()=>document.querySelector('#puzzle-message [data-sign-in]'));assert(el('.puzzle-move button').disabled,'expired puzzle can still mutate');assert(el('#puzzle-panel'),'login page replaced puzzle');`)
	guest.Run(`const res=await fetch(el('#match-panel').dataset.base+'/export/pgn');assert(res.ok,'cloud sign-out revoked independent guest');assert((await res.text()).includes('1. e4 e5'),'guest history changed after cloud sign-out');`)
	owner.AssertNoErrors()
	puzzleBrowser.AssertNoErrors()
	guest.AssertNoErrors()
	t.Log("PASS: real Caddy forward-auth/callback, virtual WebAuthn bootstrap/login, signed Chess identity/admin role, anonymous exchange and two-sided moves/PGN; two round-robin Chess processes; replica restarts during match and branching puzzle; durable all-lines completion bit; real Gateway sign-out stops owner/puzzle controls with native sign-in links and preserves guest access")
}

func liveSessionBrowser(t *testing.T, source *browser.Browser, origins ...string) *browser.Browser {
	t.Helper()
	var snapshot struct {
		Cookies []map[string]any `json:"cookies"`
	}
	if json.Unmarshal(source.Call("Network.getCookies", map[string]any{"urls": origins}), &snapshot) != nil {
		t.Fatal("cannot read private fixture session")
	}
	var cookies []map[string]any
	for _, item := range snapshot.Cookies {
		copy := map[string]any{}
		for _, key := range []string{"name", "value", "domain", "path", "secure", "httpOnly", "sameSite"} {
			if value, ok := item[key]; ok {
				copy[key] = value
			}
		}
		if expiry, ok := item["expires"].(float64); ok && expiry > 0 {
			copy["expires"] = expiry
		}
		cookies = append(cookies, copy)
	}
	b := browser.Start(nil)
	t.Cleanup(b.Close)
	b.Call("Network.setCookies", map[string]any{"cookies": cookies})
	return b
}

func liveWait(t *testing.T, b *browser.Browser, expression string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if string(b.Eval("document.readyState==='complete'&&!!("+expression+")")) == "true" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("live fixture navigation did not reach its expected page")
}

func restartLiveReplica(t *testing.T) {
	t.Helper()
	name := os.Getenv("CHESS_LIVE_RESTART_CONTAINER")
	if !strings.HasPrefix(name, "chess-live-") || !strings.HasSuffix(name, "-chess-replica") {
		t.Fatal("restart requires the disposable live fixture replica")
	}
	if err := exec.CommandContext(t.Context(), "docker", "restart", "--time", "5", name).Run(); err != nil {
		t.Fatal("unable to restart disposable Chess replica")
	}
	for range 60 {
		if exec.CommandContext(t.Context(), "docker", "exec", name, "/usr/local/bin/chess", "healthcheck").Run() == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("restarted Chess replica did not become ready")
}
