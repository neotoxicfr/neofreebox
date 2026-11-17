// Menu on small screens: the summary button opens and closes the sidebar.
document.documentElement.classList.add('js');
document.addEventListener('DOMContentLoaded', () => {
  const toggle = document.querySelector('.menu-toggle');
  if (toggle) {
    toggle.addEventListener('toggle', () => document.documentElement.classList.toggle('menu-open', toggle.open));
  }
});
