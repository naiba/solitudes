(() => {
  'use strict';
  const sidebar = document.getElementById('admin-sidebar');
  const workspace = document.getElementById('admin-workspace');
  if (!sidebar || !workspace) return;
  const toggle = document.querySelector('[data-testid="admin-nav-toggle"]');
  const close = document.querySelector('[data-testid="admin-nav-close"]');
  const backdrop = document.querySelector('.sidebar-backdrop');
  const mobile = matchMedia('(max-width: 1023px)');
  let opened = false;
  function setNavigation(open, restoreFocus = false) {
    opened = open && mobile.matches;
    sidebar.classList.toggle('is-open', opened);
    sidebar.inert = mobile.matches && !opened;
    if (opened) {
      sidebar.setAttribute('aria-modal', 'true');
      sidebar.setAttribute('role', 'dialog');
    } else {
      sidebar.removeAttribute('aria-modal');
      sidebar.removeAttribute('role');
    }
    workspace.inert = opened;
    backdrop.hidden = !opened;
    document.body.classList.toggle('navigation-open', opened);
    toggle.setAttribute('aria-expanded', String(opened));
    if (opened) close.focus();
    else if (restoreFocus) toggle.focus();
  }
  toggle.addEventListener('click', () => setNavigation(!opened));
  close.addEventListener('click', () => setNavigation(false, true));
  backdrop.addEventListener('click', () => setNavigation(false, true));
  sidebar.addEventListener('click', event => {
    if (event.target.closest('a')) setNavigation(false);
  });
  document.addEventListener('keydown', event => {
    if (!opened) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      setNavigation(false, true);
    }
    if (event.key === 'Tab') {
      const items = [...sidebar.querySelectorAll('a[href], button, input, select, [tabindex="0"]')].filter(el => !el.disabled && el.getClientRects().length);
      const first = items[0], last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }
  });
  mobile.addEventListener('change', () => setNavigation(false));
  setNavigation(false);
  const heading = document.querySelector('main h1');
  if (heading) document.getElementById('admin-current-page').textContent = heading.textContent;

  const themeButton = document.querySelector('[data-testid="admin-theme-toggle"]');
  const systemTheme = matchMedia('(prefers-color-scheme: dark)');
  function updateTheme(theme) {
    const previous = document.documentElement.dataset.theme;
    document.documentElement.dataset.theme = theme;
    themeButton.setAttribute('aria-pressed', String(theme === 'dark'));
    themeButton.querySelector('i').className = theme === 'dark' ? 'fa-regular fa-sun' : 'fa-regular fa-moon';
    if (previous !== theme) document.dispatchEvent(new CustomEvent('solitudes:theme', { detail: theme }));
  }
  updateTheme(document.documentElement.dataset.theme);
  themeButton.addEventListener('click', () => {
    const theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem('solitudes_theme', theme); } catch (_) {}
    updateTheme(theme);
  });
  systemTheme.addEventListener('change', event => {
    let preference = 'auto';
    try { preference = localStorage.getItem('solitudes_theme') || 'auto'; } catch (_) {}
    if (preference !== 'dark' && preference !== 'light') updateTheme(event.matches ? 'dark' : 'light');
  });
  window.addEventListener('storage', event => {
    if (event.key === 'solitudes_theme') updateTheme(event.newValue === 'dark' || (event.newValue !== 'light' && systemTheme.matches) ? 'dark' : 'light');
  });
})();
