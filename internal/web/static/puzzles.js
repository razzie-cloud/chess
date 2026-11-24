(() => {
  'use strict';
  function initialize() {
    const lifecycle = new AbortController();
    const signal = lifecycle.signal;
    const previews = [];
    const boards = new Map();
    let refreshController, moveController;
    const cleanup = () => {
      lifecycle.abort(); refreshController?.abort(); moveController?.abort();
      previews.forEach(({board, observer}) => { observer.disconnect(); board.destroy(); });
      boards.forEach(({board, observer}) => { observer.disconnect(); board.destroy(); });
    };
    document.addEventListener('chess:unmount', cleanup, {once: true, signal});
    addEventListener('pagehide', cleanup, {once: true, signal});
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    document.querySelectorAll('.puzzle-preview').forEach(element => {
      const board = Chessboard(element, {position: element.dataset.fen, orientation: element.dataset.color === 'b' ? 'black' : 'white', pieceTheme: '/static/pieces/{piece}.png'});
      const observer = new ResizeObserver(() => board.resize()); observer.observe(element);
      previews.push({board, observer});
    });
    const panel = document.querySelector('#puzzle-panel');
    if (!panel) return;
    const attempt = panel.dataset.attempt;
    const state = document.querySelector('#attempt-state');
    const message = document.querySelector('#puzzle-message');
    const promotion = document.querySelector('#puzzle-promotion');
    let pending = false, stopped = false, selected, choice, previousFocus, generation = 0;
    const base = '/puzzle-attempts/' + attempt;
    const expireAccess = signIn => {
      stopped = true; pending = false; refreshController?.abort(); moveController?.abort();
      if (promotion.open) promotion.close();
      controls();
      message.replaceChildren(signIn ? 'Your cloud session expired. ' : 'This attempt is no longer available.');
      if (signIn) {
        const link = document.createElement('a'); link.href = base; link.textContent = 'Sign in again'; link.dataset.signIn = '';
        message.append(link);
      }
    };
    const select = id => {
      selected = id;
      const branch = document.querySelector('#branch-' + id);
      if (!branch) return;
      branch.querySelector('input[type=radio]').checked = true;
      document.querySelector('[data-puzzle-editor]').href = '/create?' + new URLSearchParams({fen: branch.querySelector('.branch-state').dataset.fen});
      document.querySelector('[data-puzzle-gif]').href = base + '/branches/' + id + '/export/gif';
    };
    const submit = (element, uci) => {
      if (pending || stopped) return;
      const form = element.querySelector('form');
      if (form && !form.querySelector('button').disabled) { form.elements.uci.value = uci; form.requestSubmit(); }
    };
    const controls = () => {
      state.querySelectorAll('.branch').forEach(element => {
        element.querySelectorAll('form button').forEach(button => {
          button.disabled = pending || stopped || state.dataset.status !== 'active' || element.querySelector('.branch-state').dataset.status !== 'pending';
        });
      });
      panel.querySelector('form[action$="/end"] button').disabled = pending || stopped || state.dataset.status !== 'active';
    };
    const restore = element => {
      const id = element.dataset.branch;
      if (boards.has(id)) { boards.get(id).board.position(element.querySelector('.branch-state').dataset.fen, false); highlight(element); }
    };
    const highlight = element => {
      element.querySelectorAll('.last-move').forEach(square => square.classList.remove('last-move'));
      element.querySelectorAll('.in-check').forEach(square => square.classList.remove('in-check'));
      const check = element.querySelector('.branch-state').dataset.check;
      if (check) element.querySelector('[data-square="' + check + '"]')?.classList.add('in-check');
      const path = JSON.parse(element.querySelector('.branch-state').dataset.path || '[]');
      const last = path.at(-1);
      if (last) [last.slice(0, 2), last.slice(2, 4)].forEach(square => element.querySelector('[data-square="' + square + '"]')?.classList.add('last-move'));
      element.querySelectorAll('.annotated').forEach(square => square.classList.remove('annotated'));
      boards.get(element.dataset.branch)?.annotations.forEach(square => element.querySelector('[data-square="' + square + '"]')?.classList.add('annotated'));
    };
    const castles = element => {
      const form = element.querySelector('form');
      if (!form) return;
      let container = form.querySelector('.puzzle-castles');
      if (!container) { container = document.createElement('div'); container.className = 'puzzle-castles button-row'; form.append(container); }
      container.replaceChildren();
      const data = element.querySelector('.branch-state').dataset;
      const pieces = Chessboard.fenToObj(data.fen);
      (JSON.parse(data.legal || '[]') || []).filter(move => pieces[move.slice(0, 2)] === element.dataset.solver + 'K' && pieces[move.slice(2, 4)] === element.dataset.solver + 'R').forEach(move => {
        const button = document.createElement('button'); button.type = 'button'; button.textContent = 'Castle ' + move;
        button.addEventListener('click', () => { select(element.dataset.branch); submit(element, move); }); container.append(button);
      });
    };
    const initialize = element => {
      const id = element.dataset.branch;
      if (boards.has(id)) return;
      const read = () => element.querySelector('.branch-state').dataset;
      const board = Chessboard(element.querySelector('.board'), {position: read().fen, orientation: element.dataset.solver === 'b' ? 'black' : 'white', draggable: true, pieceTheme: '/static/pieces/{piece}.png', moveSpeed: reduced ? 0 : 150,
        onDragStart: (_, piece) => !pending && !stopped && state.dataset.status === 'active' && read().status === 'pending' && piece[0] === element.dataset.solver,
        onDrop: (from, to) => {
          const legal = JSON.parse(read().legal || '[]') || [];
          const options = legal.filter(move => move.startsWith(from + to));
          if (!options.length) return 'snapback';
          select(id);
          if (options.some(move => move.length === 5)) {
            choice = {id, move: from + to, revision: state.dataset.revision}; previousFocus = document.activeElement; promotion.showModal(); return 'snapback';
          }
          submit(element, options[0]);
        }
      });
      const observer = new ResizeObserver(() => { board.resize(); highlight(element); }); observer.observe(element.querySelector('.board'));
      const annotations = new Set();
      boards.set(id, {board, observer, annotations});
      element.querySelector('input[type=radio]').addEventListener('change', () => select(id));
      element.querySelector('.board').addEventListener('contextmenu', event => {
        const square = event.target.closest('[data-square]');
        if (square) { event.preventDefault(); const id = square.dataset.square; if (annotations.has(id)) annotations.delete(id); else annotations.add(id); highlight(element); }
      });
      element.querySelector('form')?.addEventListener('submit', event => { event.preventDefault(); send(event.currentTarget); });
      highlight(element);
      castles(element);
    };
    const apply = (html, expectedGeneration) => {
      const doc = new DOMParser().parseFromString(html, 'text/html');
      const next = doc.querySelector('#attempt-state');
      if (!next) throw new Error('Attempt state unavailable');
      if (expectedGeneration !== generation || Number(next.dataset.revision) < Number(state.dataset.revision) || !panel.isConnected) return;
      if (promotion.open && next.dataset.revision !== state.dataset.revision) promotion.close();
      state.dataset.revision = next.dataset.revision;
      state.dataset.status = next.dataset.status;
      state.querySelector('.attempt-summary').textContent = next.querySelector('.attempt-summary').textContent;
      next.querySelectorAll('.branch').forEach(incoming => {
        const id = incoming.dataset.branch;
        const existing = document.querySelector('#branch-' + id);
        if (!existing) {
          htmx.swap(state.querySelector('.branches'), incoming.outerHTML, {swapStyle: 'beforeend'});
          initialize(document.querySelector('#branch-' + id));
          return;
        }
        const oldState = existing.querySelector('.branch-state');
        const newState = incoming.querySelector('.branch-state');
        const changed = oldState.dataset.fen !== newState.dataset.fen;
        htmx.swap(oldState, newState.outerHTML, {swapStyle: 'outerHTML'});
        existing.className = incoming.className;
        existing.dataset.parent = incoming.dataset.parent;
        const form = existing.querySelector('form');
        const nextForm = incoming.querySelector('form');
        if (form && nextForm) {
          form.elements.revision.value = nextForm.elements.revision.value;
          form.elements.path.value = nextForm.elements.path.value;
          form.elements.uci.value = '';
        } else if (form) form.remove();
        if (changed) boards.get(id).board.position(newState.dataset.fen, !reduced);
        highlight(existing);
        castles(existing);
      });
      panel.querySelector('form[action$="/end"] input[name=revision]').value = state.dataset.revision;
      controls();
      if (selected) select(selected);
    };
    const report = async (response, expectedGeneration, recoverConflict = true) => {
      if (signal.aborted || expectedGeneration !== generation || !panel.isConnected) return false;
      if (response.type === 'opaqueredirect' || [401, 403, 404, 410].includes(response.status)) { expireAccess(response.type === 'opaqueredirect' || [401, 403].includes(response.status)); return false; }
      const text = await response.text();
      if (expectedGeneration !== generation || !panel.isConnected) return false;
      if (!response.ok) {
        const doc = new DOMParser().parseFromString(text, 'text/html');
        message.textContent = doc.querySelector('[data-error-summary]')?.textContent || 'Your access expired or Chess is unavailable. Sign in and resume this attempt.';
        if (response.status === 422) new Audio('/static/sounds/illegal.ogg').play().catch(() => {});
        if (response.status === 409 && recoverConflict) await refresh(true);
        return false;
      }
      apply(text, expectedGeneration); message.textContent = ''; return true;
    };
    const send = async form => {
      if (pending || stopped) return;
      const body = new URLSearchParams(new FormData(form));
      const expectedGeneration = ++generation;
      const previousRevision = Number(state.dataset.revision);
      const element = form.closest('.branch');
      pending = true; refreshController?.abort(); controls();
      moveController = new AbortController();
      let accepted = false;
      try {
        accepted = await report(await fetch(form.action, {method: 'POST', signal: moveController.signal, redirect: 'manual', headers: {'Content-Type': 'application/x-www-form-urlencoded', 'HX-Request': 'true'}, body}), expectedGeneration);
        if (accepted && Number(state.dataset.revision) > previousRevision) {
          const current = element.querySelector('.branch-state').dataset;
          const sound = current.status === 'solved' ? 'game-over' : current.check ? 'check' : 'move';
          new Audio('/static/sounds/' + sound + '.ogg').play().catch(() => {});
        }
      } catch (_) { if (!signal.aborted && !stopped) { message.textContent = 'Connection interrupted. Checking the saved attempt…'; await refresh(true); } }
      finally { pending = false; if (!signal.aborted) { if (!accepted && element.isConnected) restore(element); controls(); } }
    };
    const refresh = async (force = false) => {
      if (stopped || (pending && force !== true) || document.hidden) return;
      refreshController?.abort(); refreshController = new AbortController();
      const expectedGeneration = generation;
      try { await report(await fetch(base + '/state', {signal: refreshController.signal, redirect: 'manual', headers: {'HX-Request': 'true'}}), expectedGeneration, false); }
      catch (error) { if (error.name !== 'AbortError' && !stopped) message.textContent = 'Unable to refresh. Your last saved attempt is preserved.'; }
    };
    state.querySelectorAll('.branch').forEach(initialize);
    controls();
    select(state.querySelector('.branch:not([data-parent=true])').dataset.branch);
    promotion.querySelectorAll('[data-piece]').forEach(button => button.addEventListener('click', () => {
      const saved = choice, piece = button.dataset.piece; promotion.close();
      if (saved && piece !== 'cancel' && saved.revision === state.dataset.revision) submit(document.querySelector('#branch-' + saved.id), saved.move + piece);
    }));
    promotion.addEventListener('close', () => { choice = null; previousFocus?.focus(); });
    const copy = async text => {
      try { await navigator.clipboard.writeText(text); }
      catch (_) { const dialog = document.querySelector('#clipboard-fallback'); dialog.querySelector('textarea').value = text; dialog.showModal(); dialog.querySelector('textarea').select(); }
    };
    document.querySelector('[data-puzzle-close]').addEventListener('click', () => document.querySelector('#clipboard-fallback').close());
    panel.addEventListener('click', async event => {
      const button = event.target.closest('[data-puzzle-action]'); if (!button) return;
      const action = button.dataset.puzzleAction;
      if (action === 'flip') { boards.get(selected).board.flip(); highlight(document.querySelector('#branch-' + selected)); }
      if (action === 'fen') await copy(document.querySelector('#branch-' + selected + ' .branch-state').dataset.fen);
      if (action === 'link') { await copy(location.origin + base); message.textContent = 'This personal attempt link still requires your cloud account.'; }
      if (action === 'viewers') message.textContent = message.textContent === 'Personal puzzle attempt · no public viewers.' ? '' : 'Personal puzzle attempt · no public viewers.';
      if (action === 'pgn') { try { const response = await fetch(base + '/branches/' + selected + '/export/pgn', {redirect: 'manual'}); if (response.type === 'opaqueredirect' || [401, 403].includes(response.status)) { expireAccess(true); return; } if (!response.ok) throw new Error(); await copy(await response.text()); } catch (_) { message.textContent = 'The selected branch export is unavailable.'; } }
    });
    addEventListener('focus', refresh, {signal});
    document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(); }, {signal});
  }
  addEventListener('DOMContentLoaded', initialize, {once: true});
  document.addEventListener('chess:mount', initialize);
})();
