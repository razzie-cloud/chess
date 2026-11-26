/* Static proposal: board changes stay in this page; HTMX loads authored receipts. */
(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const start = 'rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR';
  const fields = ['side', 'castle', 'ep', 'half', 'full'].map($);
  let board;
  const sync = () => {
    if (board) $('fen').value = [board.fen(), ...fields.map(input => input.value || '-')].join(' ');
    $('position-feedback').textContent = '';
    $('create-result').replaceChildren();
  };
  board = Chessboard($('position-board'), {
    position: start, draggable: true, dropOffBoard: 'trash', sparePieces: true,
    pieceTheme: $('position-board').dataset.pieces, onChange: sync,
    onDragStart: () => document.querySelector('[name=preview-source]:checked').value === 'board'
  });
  sync();
  new ResizeObserver(() => board.resize()).observe($('position-board'));
  fields.forEach(input => input.addEventListener('change', sync));
  $('flip').addEventListener('click', () => board.flip());
  const resetRules = () => fields.forEach((input, i) => { input.value = ['w', '-', '-', '0', '1'][i]; });
  $('clear').addEventListener('click', () => { resetRules(); board.clear(false); sync(); });
  $('reset').addEventListener('click', () => { resetRules(); $('castle').value = 'KQkq'; board.start(false); sync(); });
  document.querySelectorAll('[name=preview-source]').forEach(input => input.addEventListener('change', () => {
    ['board', 'fen', 'pgn'].forEach(source => { $(source + '-options').hidden = source !== input.value; });
    // Spare-piece rows belong only to the visual editor.
    $('position-board').classList.toggle('preview-only', input.value !== 'board');
    $('position-feedback').textContent = '';
    $('create-result').replaceChildren();
  }));
  $('load-fen').addEventListener('click', () => {
    const parts = $('fen').value.trim().split(/\s+/);
    const ranks = (parts[0] || '').split('/');
    const positionOK = ranks.length === 8 && ranks.every(rank => /^[prnbqkPRNBQK1-8]+$/.test(rank) && [...rank].reduce((sum, value) => sum + (/\d/.test(value) ? Number(value) : 1), 0) === 8);
    if (parts.length !== 6 || !positionOK || !/^[wb]$/.test(parts[1]) || !/^(?:-|[KQkqA-Ha-h]+)$/.test(parts[2]) || !/^(?:-|[a-h][36])$/.test(parts[3]) || !/^\d{1,5}$/.test(parts[4]) || !/^[1-9]\d{0,4}$/.test(parts[5])) {
      $('position-feedback').textContent = 'Check the FEN format: eight ranks and six space-separated fields. Your draft is kept.';
      return;
    }
    fields.forEach((input, i) => { input.value = parts[i + 1]; });
    board.position(parts[0], false);
    $('position-feedback').textContent = 'Board preview updated. Position legality has not been checked.';
  });
  $('custom-game').addEventListener('input', event => {
    if (!event.target.closest('.source-switch')) $('create-result').replaceChildren();
  });
  document.addEventListener('htmx:responseError', () => {
    $('create-result').textContent = 'The preview receipt could not load. Your inputs are kept; try again.';
  });
})();
