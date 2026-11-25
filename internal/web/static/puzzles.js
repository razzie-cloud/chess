(() => {
  'use strict';
  function initialize() {
    const lifecycle = new AbortController(), signal = lifecycle.signal, boards = new Map();
    let request, preview;
    const cleanup = () => {
      lifecycle.abort(); request?.abort(); preview?.destroy();
      boards.forEach(({board, observer}) => { observer.disconnect(); board.destroy(); });
    };
    document.addEventListener('chess:unmount', cleanup, {once: true, signal});
    addEventListener('pagehide', cleanup, {once: true, signal});
    const tooltip = document.querySelector('#puzzle-tooltip');
    if (tooltip) {
      let active;
      const hide = () => { tooltip.hidden = true; active?.removeAttribute('aria-describedby'); active = null; };
      const show = link => {
        if (active === link) return;
        hide(); active = link; tooltip.hidden = false;
        link.setAttribute('aria-describedby', tooltip.id);
        if (!preview) preview = Chessboard('puzzle-tooltip-board', {pieceTheme: '/static/pieces/{piece}.png'});
        preview.orientation(link.dataset.previewColor === 'b' ? 'black' : 'white');
        preview.position(link.dataset.previewFen, false); preview.resize();
        tooltip.querySelector('small').textContent = link.getAttribute('aria-label');
        const rect = link.getBoundingClientRect(), box = tooltip.getBoundingClientRect();
        tooltip.style.left = Math.max(8, Math.min(rect.left, document.documentElement.clientWidth - box.width - 8)) + 'px';
        tooltip.style.top = (rect.bottom + box.height + 8 <= innerHeight ? rect.bottom + 6 : Math.max(8, rect.top - box.height - 6)) + 'px';
      };
      document.querySelectorAll('[data-preview-fen]').forEach(link => {
        link.addEventListener('mouseenter', () => show(link), {signal});
        link.addEventListener('mouseleave', hide, {signal});
        link.addEventListener('focus', () => show(link), {signal});
        link.addEventListener('blur', hide, {signal});
      });
      document.addEventListener('keydown', event => { if (event.key === 'Escape') hide(); }, {signal});
      addEventListener('scroll', () => {
        if (active && document.activeElement === active) { const link = active; hide(); show(link); }
        else hide();
      }, {signal, passive: true});
      addEventListener('resize', hide, {signal});
    }
    const panel = document.querySelector('#puzzle-panel');
    if (!panel) return;
    const state = panel.querySelector('#puzzle-state'), message = panel.querySelector('#puzzle-message'), promotion = panel.querySelector('#puzzle-promotion');
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    let pending = false, stopped = false, choice, previousFocus, generation = 0;
    const controls = () => state.querySelectorAll('form button, form input').forEach(control => { control.disabled = pending || stopped; });
    const highlight = element => {
      element.querySelectorAll('.last-move,.in-check,.annotated').forEach(square => square.classList.remove('last-move', 'in-check', 'annotated'));
      const data = element.querySelector('.branch-state').dataset;
      if (data.check) element.querySelector('[data-square="' + data.check + '"]')?.classList.add('in-check');
      const last = JSON.parse(data.path).at(-1);
      if (last) [last.slice(0, 2), last.slice(2, 4)].forEach(square => element.querySelector('[data-square="' + square + '"]')?.classList.add('last-move'));
      boards.get(element.dataset.branch)?.annotations.forEach(square => element.querySelector('[data-square="' + square + '"]')?.classList.add('annotated'));
    };
    const restore = element => { boards.get(element.dataset.branch)?.board.position(element.querySelector('.branch-state').dataset.fen, false); highlight(element); };
    const submit = (element, uci) => {
      if (pending || stopped) return;
      const form = element.querySelector('form');
      if (form) { form.elements.uci.value = uci; form.requestSubmit(); }
    };
    const castles = element => {
      const container = element.querySelector('.puzzle-castles');
      if (!container) return;
      container.replaceChildren();
      const data = element.querySelector('.branch-state').dataset, pieces = Chessboard.fenToObj(data.fen);
      (JSON.parse(data.legal || '[]') || []).filter(move => pieces[move.slice(0, 2)] === element.dataset.solver + 'K' && pieces[move.slice(2, 4)] === element.dataset.solver + 'R').forEach(move => {
        const button = document.createElement('button'); button.type = 'button'; button.textContent = 'Castle ' + move;
        button.addEventListener('click', () => submit(element, move), {signal}); container.append(button);
      });
    };
    const mount = element => {
      const id = element.dataset.branch;
      if (boards.has(id)) return;
      const read = () => element.querySelector('.branch-state').dataset;
      const board = Chessboard(element.querySelector('.board'), {position: read().fen, orientation: element.dataset.solver === 'b' ? 'black' : 'white', draggable: true, pieceTheme: '/static/pieces/{piece}.png', moveSpeed: reduced ? 0 : 150,
        onDragStart: (_, piece) => !pending && !stopped && read().done !== 'true' && piece[0] === element.dataset.solver,
        onDrop: (from, to) => {
          const options = (JSON.parse(read().legal || '[]') || []).filter(move => move.startsWith(from + to));
          if (!options.length) return 'snapback';
          if (options.some(move => move.length === 5)) {
            choice = {id, move: from + to, generation}; previousFocus = document.activeElement; promotion.showModal(); return 'snapback';
          }
          submit(element, options[0]);
        }
      });
      const observer = new ResizeObserver(() => { board.resize(); highlight(element); }); observer.observe(element.querySelector('.board'));
      const annotations = new Set(); boards.set(id, {board, observer, annotations});
      element.querySelector('.board').addEventListener('contextmenu', event => {
        const square = event.target.closest('[data-square]');
        if (square) { event.preventDefault(); const name = square.dataset.square; if (annotations.has(name)) annotations.delete(name); else annotations.add(name); highlight(element); }
      }, {signal});
      element.querySelector('form')?.addEventListener('submit', event => { event.preventDefault(); send(event.currentTarget); }, {signal});
      highlight(element); castles(element);
    };
    const apply = html => {
      const next = new DOMParser().parseFromString(html, 'text/html').querySelector('#puzzle-state');
      if (!next) throw new Error('Puzzle state unavailable');
      if (promotion.open) promotion.close();
      const overview = state.querySelector('#puzzle-overview'), incomingOverview = next.querySelector('#puzzle-overview');
      overview.remove();
      incomingOverview.remove();
      const incomingIDs = new Set([...next.querySelectorAll('.branch')].map(element => element.dataset.branch));
      state.querySelectorAll('.branch').forEach(element => {
        const id = element.dataset.branch;
        if (!incomingIDs.has(id)) { const saved = boards.get(id); saved.observer.disconnect(); saved.board.destroy(); boards.delete(id); element.remove(); }
      });
      next.querySelectorAll('.branch').forEach(incoming => {
        const id = incoming.dataset.branch, existing = state.querySelector('#branch-' + id);
        if (!existing) { state.querySelector('.branches').append(incoming); mount(incoming); return; }
        const before = existing.querySelector('.branch-state'), after = incoming.querySelector('.branch-state');
        const changed = before.dataset.fen !== after.dataset.fen;
        before.replaceWith(after); existing.className = incoming.className;
        if (incoming.querySelector('form')) existing.querySelector('form').elements.uci.value = '';
        else existing.querySelector('form')?.remove();
        if (changed) boards.get(id).board.position(after.dataset.fen, !reduced);
        highlight(existing); castles(existing);
      });
      state.querySelector('.puzzle-sidebar').prepend(overview);
      state.dataset.state = next.dataset.state; state.dataset.revision = next.dataset.revision; state.dataset.done = next.dataset.done;
      state.querySelector('.puzzle-summary').replaceWith(incomingOverview.querySelector('.puzzle-summary'));
      state.querySelector('#puzzle-navigation').replaceWith(incomingOverview.querySelector('#puzzle-navigation'));
      htmx.process(state);
      document.dispatchEvent(new CustomEvent('chess:navigation', {detail: state}));
    };
    const expire = () => {
      stopped = true; if (promotion.open) promotion.close();
      const link = document.createElement('a'); link.href = panel.dataset.base; link.textContent = 'Sign in again'; link.dataset.signIn = '';
      message.replaceChildren('Your cloud session expired. ', link); controls();
    };
    const send = async form => {
      if (pending || stopped) return;
      const element = form.closest('.branch'), expected = ++generation;
      const body = JSON.stringify({content_digest: panel.dataset.digest, state: JSON.parse(state.dataset.state), branch: form.elements.branch.value, uci: form.elements.uci.value});
      pending = true; controls(); request = new AbortController();
      let accepted = false;
      try {
        const response = await fetch(form.action, {method: 'POST', signal: request.signal, redirect: 'manual', headers: {'Content-Type': 'application/json', 'HX-Request': 'true'}, body});
        if (signal.aborted || expected !== generation || !panel.isConnected) return;
        if (response.type === 'opaqueredirect' || [401, 403].includes(response.status)) { expire(); return; }
        const html = await response.text();
        if (!response.ok) {
          const doc = new DOMParser().parseFromString(html, 'text/html');
          message.textContent = doc.querySelector('[data-error-summary]')?.textContent || 'Unable to verify this move. Your boards are preserved; try again.';
          if (response.status === 422) new Audio('/static/sounds/illegal.ogg').play().catch(() => {});
          return;
        }
        apply(html); accepted = true; message.textContent = '';
        new Audio('/static/sounds/' + (state.dataset.done === 'true' ? 'game-over' : 'move') + '.ogg').play().catch(() => {});
      } catch (error) { if (error.name !== 'AbortError' && !signal.aborted) message.textContent = 'Connection interrupted. Your boards are preserved; retry the move.'; }
      finally { pending = false; if (!signal.aborted) { if (!accepted && element.isConnected) restore(element); controls(); } }
    };
    state.querySelectorAll('.branch').forEach(mount);
    promotion.querySelectorAll('[data-piece]').forEach(button => button.addEventListener('click', () => {
      const saved = choice, piece = button.dataset.piece; promotion.close();
      if (saved && piece !== 'cancel' && saved.generation === generation) submit(state.querySelector('#branch-' + saved.id), saved.move + piece);
    }, {signal}));
    promotion.addEventListener('close', () => { choice = null; previousFocus?.focus(); }, {signal});
  }
  addEventListener('DOMContentLoaded', initialize, {once: true});
  document.addEventListener('chess:mount', initialize);
})();
