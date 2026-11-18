// Menu on small screens: the summary opens and closes the sidebar. While it
// is open, the page behind is inert (focus stays in the menu) and Escape
// closes it.
document.documentElement.classList.add('js');
document.addEventListener('DOMContentLoaded', () => {
  const toggle = document.querySelector('.menu-toggle');
  const main = document.getElementById('main');
  if (!toggle) return;
  toggle.addEventListener('toggle', () => {
    document.documentElement.classList.toggle('menu-open', toggle.open);
    if (main) main.inert = toggle.open;
  });
  // Widening past the phone layout hides the menu button: close the menu so
  // the page does not stay inert behind an invisible control.
  const wide = window.matchMedia('(min-width: 861px)');
  wide.addEventListener('change', () => {
    if (wide.matches && toggle.open) toggle.open = false;
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && toggle.open) {
      toggle.open = false;
      toggle.querySelector('summary').focus();
    }
  });
});

// A table wider than its frame keeps its horizontal scroll when a row's panel
// opens (see .table-wrap.scrolls in app.css). Measured on the click that opens
// it (Enter and Space on a summary click too), before the layout changes.
document.addEventListener('click', (e) => {
  const summary = e.target.closest && e.target.closest('details.inline > summary');
  const wrap = summary && summary.closest('.table-wrap');
  if (wrap && !summary.parentElement.open) {
    wrap.classList.toggle('scrolls', wrap.scrollWidth > wrap.clientWidth + 1);
  }
}, true);
