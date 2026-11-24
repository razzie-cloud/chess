(() => {
  'use strict';
  if (location.pathname !== '/g') return;
  let token = location.hash.slice(1);
  history.replaceState(null, '', '/g');
  addEventListener('DOMContentLoaded', () => {
    const button = document.querySelector('#guest-continue');
    const message = document.querySelector('#guest-message');
    if (!/^[a-f0-9]{64}$/.test(token)) {
      token = '';
      button.disabled = true;
      message.textContent = 'Reopen the original playing link to continue.';
    }
    button.addEventListener('click', async () => {
      button.disabled = true;
      try {
        const response = await fetch('/guest-sessions', {method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json'}, body: new URLSearchParams({token})});
        if (!response.ok) throw new Error('This playing link is unavailable. Ask the creator for a current link.');
        const result = await response.json();
        token = '';
        location.replace(result.url);
      } catch (error) {
        message.textContent = error.message;
        button.disabled = false;
      }
    });
  });
})();
