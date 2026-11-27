(() => {
  'use strict';
  let teardown = () => {};
  const initialize = () => {
    teardown();
    const lifecycle = new AbortController();
    const cleaners = [() => fireworks.stop()];
    teardown = () => { lifecycle.abort(); cleaners.splice(0).forEach(clean => clean()); };
    const on = (target, type, listener, options = {}) => target.addEventListener(type, listener, {...options, signal: lifecycle.signal});
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const uid = () => crypto.randomUUID();
    document.querySelectorAll('form input[name=request_id]').forEach(input => { input.value = uid(); });
    let dirty = false;
    document.querySelectorAll('[data-draft]').forEach(form => {
      form.addEventListener('input', () => { dirty = true; });
      form.addEventListener('submit', () => { dirty = false; });
    });
    on(window, 'beforeunload', event => { if (dirty) { event.preventDefault(); event.returnValue = ''; } });
    on(document, 'click', event => {
      const link = event.target.closest('a[href]');
      if (!link || !dirty || link.getAttribute('href').startsWith('#')) return;
      if (!confirm('Leave this unsaved position or PGN draft?')) { event.preventDefault(); event.stopPropagation(); }
      else dirty = false;
    }, {capture: true});
    on(document, 'htmx:confirm', event => {
      if (!dirty || !event.detail.elt.closest('a[href]')) return;
      event.preventDefault();
      if (confirm('Leave this unsaved position or PGN draft?')) { dirty = false; event.detail.issueRequest(true); }
    });
    document.querySelectorAll('[data-confirm]').forEach(form => form.addEventListener('submit', event => { if (!confirm(form.dataset.confirm)) event.preventDefault(); }));
    document.querySelector('[data-error-summary]')?.focus();

    const opponent = document.querySelector('#setup-opponent');
    if (opponent) {
      const selectOpponent = () => {
        const bot = opponent.value === 'bot';
        document.querySelectorAll('[data-guest-fields]').forEach(panel => { panel.hidden = bot; });
        document.querySelectorAll('[data-bot-fields]').forEach(panel => { panel.hidden = !bot; });
        const name = document.querySelector('#setup input[name=name]');
        name.required = !bot;
        document.querySelector('#setup select[name=bot_level]').disabled = !bot;
      };
      on(opponent, 'change', selectOpponent);
      selectOpponent();
    }

    const editor = document.querySelector('#editor-board');
    const preview = document.querySelector('#setup-board');
    if (editor || preview) {
      const color = () => document.querySelector('#setup input[name=color]:checked')?.value === 'b' ? 'black' : 'white';
      let board;
      if (editor) {
        const fen = document.querySelector('#edit-fen');
        const fields = ['side', 'castle', 'ep', 'half', 'full'].map(name => document.querySelector('#edit-' + name));
        let loading = false;
        const sync = () => {
          if (!board || loading) return;
          fen.value = [board.fen(), ...fields.map(input => input.value || '-')].join(' ');
          dirty = true;
        };
        const parts = fen.value.trim().split(/\s+/);
        board = Chessboard(editor, {
          position: Chessboard.fenToObj(parts[0]) || 'start', orientation: color(),
          draggable: true, dropOffBoard: 'trash', sparePieces: true,
          pieceTheme: '/static/pieces/{piece}.png', onChange: sync,
          onDragStart: () => document.querySelector('#setup input[name=source]:checked').value === 'board'
        });
        const load = () => {
          const parts = fen.value.trim().split(/\s+/);
          if (parts.length !== 6 || !Chessboard.fenToObj(parts[0])) return;
          fields.forEach((field, i) => { field.value = parts[i + 1]; });
          // Previewing must not rewrite a malformed FEN draft through sanitized controls.
          loading = true;
          board.position(parts[0], false);
          loading = false;
        };
        load();
        const selectSource = () => {
          const source = document.querySelector('#setup input[name=source]:checked').value;
          fields.forEach(field => { field.disabled = source !== 'board'; });
          document.querySelector('#edit-pgn').required = source === 'pgn';
        };
        document.querySelectorAll('#setup input[name=source]').forEach(input => on(input, 'change', selectSource));
        selectSource();
        fields.forEach(input => on(input, 'change', sync));
        on(fen, 'change', load);
        document.querySelectorAll('[data-editor]').forEach(button => on(button, 'click', () => {
          const start = button.dataset.editor === 'start';
          fields.forEach((field, i) => { field.value = ['w', start ? 'KQkq' : '-', '-', '0', '1'][i]; });
          if (start) board.start(false); else board.clear(false);
          sync();
        }));
      } else {
        board = Chessboard(preview, {position: preview.dataset.fen, orientation: color(), pieceTheme: '/static/pieces/{piece}.png'});
      }
      const refresh960 = document.querySelector('#refresh-chess960');
      if (preview && refresh960) {
        cleaners.push(() => htmx.trigger(refresh960, 'htmx:abort'));
        const feedback = document.querySelector('#chess960-feedback');
        let restoreRefreshFocus = false;
        on(document, 'htmx:beforeRequest', event => {
          if (event.detail.elt === refresh960) restoreRefreshFocus = document.activeElement === refresh960;
        });
        on(document, 'htmx:afterRequest', event => {
          if (event.detail.elt !== refresh960) return;
          // Disabling the button can blur it; keep focus if the user moved elsewhere.
          if (restoreRefreshFocus && refresh960.isConnected && document.activeElement === document.body) refresh960.focus({preventScroll: true});
          restoreRefreshFocus = false;
        });
        on(document, 'htmx:afterSwap', event => {
          if (event.detail.target?.id !== 'chess960-position') return;
          const position = document.querySelector('#chess960-position');
          preview.dataset.fen = position.dataset.fen;
          board.position(position.dataset.fen, false);
          dirty = true;
          feedback.textContent = 'New starting position ready.';
        });
        const refreshFailed = event => {
          if (event.detail.elt?.id === 'refresh-chess960') feedback.textContent = 'Unable to refresh. Your current position is kept; try again.';
        };
        on(document, 'htmx:responseError', refreshFailed);
        on(document, 'htmx:sendError', refreshFailed);
        on(document, 'htmx:timeout', refreshFailed);
      }
      on(document.querySelector('[data-setup-flip]'), 'click', () => board.flip());
      document.querySelectorAll('#setup input[name=color]').forEach(input => on(input, 'change', () => board.orientation(color())));
      const observer = new ResizeObserver(() => board.resize());
      observer.observe(editor || preview);
      cleaners.push(() => { observer.disconnect(); board.destroy(); });
    }

    async function copy(text) {
      try { await navigator.clipboard.writeText(text); }
      catch (_) {
        const dialog = document.querySelector('#clipboard-fallback');
        if (!dialog) { prompt('Copy this text', text); return; }
        dialog.querySelector('textarea').value = text;
        dialog.showModal();
        dialog.querySelector('textarea').select();
      }
    }
    document.querySelector('[data-copy-playing]')?.addEventListener('click', () => copy(document.querySelector('[data-playing-link]').value));
    document.querySelector('[data-close]')?.addEventListener('click', event => event.target.closest('dialog').close());

    document.querySelectorAll('[data-match-fen]').forEach(element => {
      const board = Chessboard(element, {position: element.dataset.matchFen, orientation: element.dataset.color === 'b' ? 'black' : 'white', showNotation: false, pieceTheme: '/static/pieces/{piece}.png'});
      const observer = new ResizeObserver(() => board.resize());
      observer.observe(element);
      cleaners.push(() => { observer.disconnect(); board.destroy(); });
    });

    const panel = document.querySelector('#match-panel');
    if (!panel) return;
    const resource = panel.dataset.resource, base = panel.dataset.base;
    const boardNode = document.querySelector('#match-board');
    const form = document.querySelector('#move-form');
    const connection = document.querySelector('#connection');
    const promotion = document.querySelector('#promotion');
    const viewer = uid();
    const annotations = new Set();
    let castleKey, selectedPly;
    let state, board, timer, poll, pending = false, uncertain = false, stopped = false, failures = 0, showViewers = false, history = false, promotionMove, promotionRevision, focusBefore;
    const read = () => JSON.parse(document.querySelector('#match-state').dataset.state);
    state = read();
    const highlight = () => {
      boardNode.querySelectorAll('.in-check,.last-move,.annotated').forEach(square => square.classList.remove('in-check', 'last-move', 'annotated'));
      const entry = history ? state.history?.find(move => move.Ply === selectedPly) : state.history?.at(-1);
      let checked = history ? '' : state.checkSquare;
      if (history && entry && /[+#]/.test(entry.SAN)) {
        const pieces = Chessboard.fenToObj(entry.FEN);
        const side = entry.FEN.split(/\s+/)[1];
        checked = Object.keys(pieces).find(square => pieces[square] === side + 'K');
      }
      if (checked) boardNode.querySelector('[data-square="' + checked + '"]')?.classList.add('in-check');
      if (entry) [entry.UCI.slice(0, 2), entry.UCI.slice(2, 4)].forEach(square => boardNode.querySelector('[data-square="' + square + '"]')?.classList.add('last-move'));
      annotations.forEach(square => boardNode.querySelector('[data-square="' + square + '"]')?.classList.add('annotated'));
    };
    const expireAccess = signIn => {
      stopped = true; pending = false; uncertain = false;
      clearTimeout(timer); poll?.abort();
      if (promotion?.open) promotion.close();
      panel.querySelectorAll('form button, form input[name=uci]').forEach(control => { control.disabled = true; });
      connection.replaceChildren(signIn ? 'Your cloud session expired. ' : 'This match is no longer available or your access expired. Reopen your playing link.');
      if (signIn) {
        const link = document.createElement('a'); link.href = base; link.textContent = 'Sign in again'; link.dataset.signIn = '';
        connection.append(link);
      }
    };
    const sound = name => { new Audio('/static/sounds/' + name + '.ogg').play().catch(() => {}); };
    const reconcile = (animate = false, force = false) => {
      const next = read();
      if (next.id !== resource || next.revision < state.revision) return;
      const changed = next.fen !== state.fen;
      const ended = next.status === 'completed' && state.status !== 'completed';
      state = next;
      if (ended && ['1-0', '0-1'].includes(state.result)) fireworks.play();
      if (changed) history = false;
      if (!history && board && (changed || force)) board.position(state.fen, animate && !reduced);
      if (animate && (changed || ended)) {
        const san = state.history?.at(-1)?.SAN || '';
        sound(ended ? 'game-over' : /[+#]/.test(san) ? 'check' : san.includes('x') ? 'capture' : 'move');
      }
      if (promotion?.open && state.revision !== promotionRevision) promotion.close();
      panel.querySelectorAll('input[name=revision]').forEach(input => { input.value = state.revision; });
      if (form) {
        form.querySelector('button[type=submit]').disabled = !state.legal?.length || pending || uncertain || stopped || history;
        const castles = document.querySelector('#castle-controls');
        const nextCastleKey = JSON.stringify([state.legal || [], state.color]);
        if (castleKey !== nextCastleKey) {
          castles.replaceChildren();
          const pieces = Chessboard.fenToObj(state.fen);
          (state.legal || []).filter(move => pieces[move.slice(0, 2)]?.[1] === 'K' && pieces[move.slice(2, 4)] === state.color + 'R').forEach(move => {
            const button = document.createElement('button');
            button.type = 'button'; button.textContent = 'Castle ' + move; button.addEventListener('click', () => submit(move)); castles.append(button);
          });
          castleKey = nextCastleKey;
        }
        castles.querySelectorAll('button').forEach(button => { button.disabled = pending || uncertain || stopped || history; });
      }
      document.querySelectorAll('.viewers').forEach(item => { item.hidden = !showViewers; });
      highlight();
    };
    const submit = move => {
      if (!form || pending || uncertain || stopped || history) return;
      form.elements.uci.value = move;
      form.requestSubmit();
    };
    board = Chessboard(boardNode, {position: state.fen, orientation: state.color === 'b' ? 'black' : 'white', draggable: !!form, pieceTheme: '/static/pieces/{piece}.png', moveSpeed: reduced ? 0 : 150,
      onDragStart: (_, piece) => !pending && !uncertain && !stopped && !history && state.legal?.length && piece[0] === state.color,
      onDrop: (from, to) => {
        const options = (state.legal || []).filter(move => move.startsWith(from + to));
        if (!options.length) { sound('illegal'); return 'snapback'; }
        if (options.some(move => move.length === 5)) {
          promotionMove = from + to; promotionRevision = state.revision; focusBefore = document.activeElement; promotion.showModal();
          return 'snapback';
        }
        submit(options[0]);
      }
    });
    const observer = new ResizeObserver(() => { board.resize(); highlight(); });
    observer.observe(boardNode);
    cleaners.push(() => { stopped = true; clearTimeout(timer); poll?.abort(); observer.disconnect(); board.destroy(); });
    reconcile();
    const restorePromotion = () => { promotionMove = null; board.position(state.fen, false); focusBefore?.focus(); };
    promotion?.addEventListener('close', restorePromotion);
    promotion?.querySelectorAll('[data-promotion]').forEach(button => button.addEventListener('click', () => {
      const move = promotionMove, revision = promotionRevision, choice = button.dataset.promotion;
      promotion.close();
      if (choice !== 'cancel' && move && revision === state.revision) submit(move + choice);
    }));
    boardNode.addEventListener('contextmenu', event => {
      const square = event.target.closest('[data-square]');
      if (square) { event.preventDefault(); const name = square.dataset.square; if (annotations.has(name)) annotations.delete(name); else annotations.add(name); highlight(); }
    });
    on(document, 'click', async event => {
      if (event.target.closest('[data-flip]')) { board.flip(); highlight(); }
      if (event.target.closest('[data-viewers]')) { showViewers = !showViewers; reconcile(); }
      const historical = event.target.closest('[data-history-fen]');
      if (historical) { history = true; selectedPly = Number(historical.dataset.ply); board.position(historical.dataset.historyFen, false); if (form) form.querySelector('button[type=submit]').disabled = true; highlight(); }
      if (event.target.closest('[data-live]')) { history = false; board.position(state.fen, false); reconcile(); }
      const button = event.target.closest('[data-copy]');
      if (button) {
        if (button.dataset.copy === 'fen') await copy(state.fen);
        if (button.dataset.copy === 'link') { if (state.viewURL) await copy(state.viewURL); else connection.textContent = 'Public viewing is disabled. Ask the creator to create a viewing link.'; }
        if (button.dataset.copy === 'pgn') { try { const res = await fetch(base + '/export/pgn', {redirect: 'manual'}); if (res.type === 'opaqueredirect' || [401, 403].includes(res.status)) { expireAccess(true); return; } if (!res.ok) throw new Error(); await copy(await res.text()); } catch (_) { connection.textContent = 'The export is unavailable. Refresh your access.'; } }
      }
    });
    const schedule = () => {
      clearTimeout(timer);
      if (!stopped && !document.hidden) timer = setTimeout(refresh, failures ? Math.min(30000, 1000 * 2 ** failures) : state.legal?.length ? 5000 : 1000);
    };
    const refresh = async () => {
      if (pending || poll || stopped || document.hidden) { schedule(); return; }
      const controller = new AbortController();
      poll = controller;
      try {
        const response = await fetch(base + '/state?' + new URLSearchParams({revision: state.revision, viewers: state.viewers}), {signal: controller.signal, redirect: 'manual', headers: {'HX-Request': 'true'}});
        if (controller.signal.aborted || pending || stopped || !panel.isConnected) return;
        if (response.type === 'opaqueredirect' || [401, 403, 404, 410].includes(response.status)) { expireAccess(panel.dataset.role === 'owner' && (response.type === 'opaqueredirect' || [401, 403].includes(response.status))); return; }
        if (!response.ok) throw new Error();
        if (response.status !== 204) {
          const html = await response.text();
          if (controller.signal.aborted || pending || stopped || !panel.isConnected || !apply(html)) return;
        }
        if (uncertain) {
          uncertain = false;
          form.elements.request_id.value = uid();
          form.elements.uci.value = '';
          reconcile(false, true);
        }
        failures = 0; connection.textContent = '';
      } catch (error) { if (error.name !== 'AbortError') { failures++; connection.textContent = 'Connection interrupted. Retrying…'; } }
      finally { if (poll === controller) poll = null; schedule(); }
    };
    const apply = html => {
      const doc = new DOMParser().parseFromString(html, 'text/html');
      const incoming = doc.querySelector('#match-state');
      if (!incoming) throw new Error('Missing match state');
      const next = JSON.parse(incoming.dataset.state);
      if (next.id !== resource || next.revision < state.revision || !panel.isConnected) return false;
      const previous = document.querySelector('#match-state');
      const detailsOpen = previous.querySelector('details')?.open;
      const historyScroll = previous.querySelector('.move-history')?.scrollTop || 0;
      const focused = previous.contains(document.activeElement) ? document.activeElement : null;
      const focusSelector = focused?.dataset.ply ? '[data-ply="' + focused.dataset.ply + '"]' : focused?.hasAttribute('data-live') ? '[data-live]' : focused?.tagName === 'SUMMARY' ? 'summary' : null;
      const animate = next.revision === state.revision + 1;
      htmx.swap('#match-state', incoming.outerHTML, {swapStyle: 'outerHTML'});
      const current = document.querySelector('#match-state');
      if (current.querySelector('details')) current.querySelector('details').open = !!detailsOpen;
      if (current.querySelector('.move-history')) current.querySelector('.move-history').scrollTop = historyScroll;
      if (focusSelector) current.querySelector(focusSelector)?.focus({preventScroll: true});
      reconcile(animate);
      return true;
    };
    on(document, 'htmx:beforeRequest', event => {
      if (event.detail.elt !== form) return;
      if (uncertain || stopped) { event.preventDefault(); return; }
      pending = true; poll?.abort(); clearTimeout(timer); connection.textContent = 'Sending move…';
      form.querySelector('button[type=submit]').disabled = true;
      document.querySelectorAll('#castle-controls button').forEach(button => { button.disabled = true; });
    });
    on(document, 'htmx:beforeSwap', event => {
      if (event.detail.target.id !== 'match-state') return;
      event.detail.shouldSwap = false;
      const status = event.detail.xhr.status;
      if (status >= 200 && status < 300) {
        try {
          apply(event.detail.xhr.responseText);
          pending = false; uncertain = false;
          form.elements.request_id.value = uid(); form.elements.uci.value = ''; connection.textContent = '';
        } catch (_) {
          pending = false; uncertain = true;
          connection.textContent = 'The response could not update this match. Checking the saved position…';
          refresh();
        }
      } else {
        pending = false;
        uncertain = status === 0 || status >= 500;
        const doc = new DOMParser().parseFromString(event.detail.xhr.responseText, 'text/html');
        connection.textContent = doc.querySelector('[data-error-summary]')?.textContent || 'Move not accepted. Refreshing the position…';
        if (status === 422) sound('illegal');
        if ([401, 403, 404, 410].includes(status)) expireAccess(panel.dataset.role === 'owner' && [401, 403].includes(status));
        if (!stopped) refresh();
      }
      reconcile(false, true); schedule();
    });
    const interrupted = event => {
      if (event.detail.elt !== form) return;
      pending = false; uncertain = true;
      connection.textContent = 'The move may have arrived. Checking the saved position…';
      reconcile(false, true); refresh(); schedule();
    };
    on(document, 'htmx:sendError', interrupted);
    on(document, 'htmx:timeout', interrupted);
    on(document, 'visibilitychange', () => { if (document.hidden) { clearTimeout(timer); poll?.abort(); } else refresh(); });
    const presence = setInterval(() => { if (!document.hidden && !stopped) fetch(base + '/presence', {method: 'POST', redirect: 'manual', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body: new URLSearchParams({viewer_id: viewer})}).then(response => { if (response.type === 'opaqueredirect' || [401, 403, 404, 410].includes(response.status)) expireAccess(panel.dataset.role === 'owner' && (response.type === 'opaqueredirect' || [401, 403].includes(response.status))); }).catch(() => {}); }, 15000);
    cleaners.push(() => clearInterval(presence));
    schedule();

  };
  document.addEventListener('chess:mount', initialize);
  document.addEventListener('chess:unmount', () => teardown());
  addEventListener('pagehide', () => teardown());
  addEventListener('pageshow', event => { if (event.persisted) initialize(); });
  initialize();
})();
