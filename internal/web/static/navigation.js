(() => {
  'use strict';
  const enhance = (root, path = location.pathname) => {
    root.querySelectorAll('a[href]').forEach(link => {
      const raw = link.getAttribute('href');
      const target = new URL(link.href, location.href);
      if (!raw || raw.startsWith('#') || target.origin !== location.origin || link.hasAttribute('download') || link.target || target.pathname.includes('/export/') || target.pathname.startsWith('/static/') || location.pathname.startsWith('/g') || location.pathname.startsWith('/play/') || location.pathname.startsWith('/watch/')) return;
      link.setAttribute('hx-get', target.pathname + target.search);
      link.setAttribute('hx-target', '#main');
      link.setAttribute('hx-select', '#main');
      link.setAttribute('hx-swap', 'outerHTML show:window:top');
      link.setAttribute('hx-push-url', 'true');
      link.setAttribute('hx-sync', '#main:replace');
    });
    root.querySelectorAll('form[method=get]').forEach(form => {
      form.setAttribute('hx-get', form.action || location.pathname);
      form.setAttribute('hx-target', '#main');
      form.setAttribute('hx-select', '#main');
      form.setAttribute('hx-swap', 'outerHTML show:window:top');
      form.setAttribute('hx-push-url', 'true');
      form.setAttribute('hx-sync', '#main:replace');
    });
    const current = path.startsWith('/puzzle') ? '/puzzles' : path === '/matches/archive' ? path : path === '/matches/new' || path === '/create' ? '/matches/new' : '/matches';
    document.querySelectorAll('.app-nav [data-nav]').forEach(link => {
      if (link.dataset.nav === current) link.setAttribute('aria-current', 'page');
      else link.removeAttribute('aria-current');
    });
    htmx.process(root);
  };
  addEventListener('DOMContentLoaded', () => enhance(document));
  document.addEventListener('htmx:beforeRequest', event => {
    if (event.detail.target?.id === 'main') document.querySelectorAll('.game-menu:popover-open').forEach(menu => menu.hidePopover());
  });
  document.addEventListener('htmx:beforeSwap', event => {
    if (event.detail.target?.id !== 'main') return;
    const status = event.detail.xhr.status;
    if ([401, 403].includes(status)) { event.detail.shouldSwap = false; location.reload(); return; }
    if ([409, 422, 503].includes(status)) { event.detail.shouldSwap = true; event.detail.isError = false; }
    if (event.detail.shouldSwap) document.dispatchEvent(new Event('chess:unmount'));
  });
  document.addEventListener('htmx:afterSwap', event => {
    if (event.detail.target?.id !== 'main') return;
    enhance(document.querySelector('#main'), new URL(event.detail.xhr.responseURL || location.href).pathname);
    document.dispatchEvent(new Event('chess:mount'));
    document.querySelector('[data-error-summary]')?.focus();
  });
  document.addEventListener('chess:navigation', event => enhance(event.detail));
  const interrupted = event => {
    if (event.detail.target?.id !== 'main' && event.detail.requestConfig?.target?.id !== 'main') return;
    let notice = document.querySelector('#navigation-error');
    if (!notice) {
      notice = document.createElement('p'); notice.id = 'navigation-error'; notice.className = 'error'; notice.setAttribute('role', 'status');
      document.querySelector('#main').prepend(notice);
    }
    const destination = new URL(event.detail.elt?.href || location.href, location.href);
    const link = document.createElement('a');
    link.href = destination.origin === location.origin ? destination.href : location.href;
    link.textContent = 'Open this page to sign in again or retry.';
    notice.replaceChildren('Unable to load this page. ', link);
  };
  document.addEventListener('htmx:sendError', interrupted);
  document.addEventListener('htmx:timeout', interrupted);
  // HTMX history snapshots are disabled. A fresh document restores browser hooks
  // and identity after Back/Forward, including pages left during a move request.
  addEventListener('popstate', () => { document.dispatchEvent(new Event('chess:unmount')); location.reload(); });
})();
