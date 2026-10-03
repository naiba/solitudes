(function () {
  'use strict';
  const dialog = document.querySelector('#article-share-dialog');
  if (dialog) {
    const url = dialog.dataset.url;
    const status = dialog.querySelector('[role="status"]');
    const input = dialog.querySelector('input');
    const native = dialog.querySelector('[data-share-native]');
    let opener;
    let previousOverflow;
    native.hidden = typeof navigator.share !== 'function';
    document.querySelectorAll('[data-share-open]').forEach(button => {
      button.addEventListener('click', () => {
        if (dialog.open) return;
        opener = button;
        status.textContent = '';
        previousOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
        dialog.showModal();
        dialog.querySelector('[data-share-copy]').focus();
      });
    });
    dialog.querySelector('[data-share-close]').addEventListener('click', () => dialog.close());
    dialog.addEventListener('keydown', event => {
      if (event.key !== 'Tab') return;
      const controls = Array.from(dialog.querySelectorAll('button:not(:disabled), input, a[href]')).filter(el => el.getClientRects().length);
      const first = controls[0], last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    });
    dialog.addEventListener('click', event => {
      const rect = dialog.getBoundingClientRect();
      if (event.target === dialog && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom)) dialog.close();
    });
    dialog.addEventListener('close', () => {
      document.body.style.overflow = previousOverflow || '';
      if (opener && opener.isConnected) opener.focus({ preventScroll: true });
    });
    input.addEventListener('click', () => input.select());
    dialog.querySelector('[data-share-copy]').addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(url);
        status.textContent = dialog.dataset.copied;
      } catch (_) {
        status.textContent = dialog.dataset.hint;
        input.focus();
        input.select();
      }
    });
    native.addEventListener('click', async () => {
      native.disabled = true;
      try {
        await navigator.share({ title: dialog.dataset.title, url });
        dialog.close();
      } catch (error) {
        // Cancelling the operating-system sheet is not a failed copy request.
        if (error.name !== 'AbortError') {
          status.textContent = dialog.dataset.hint;
          input.focus();
          input.select();
        }
      } finally {
        native.disabled = false;
      }
    });
  }

  const tocs = Array.from(document.querySelectorAll('.cactus-toc'));
  const content = document.querySelector('[data-reading-content]');
  if (!tocs.length || !content) return;
  const mobilePanel = document.getElementById('toc-footer');
  const mobileToggle = document.getElementById('toc-footer-toggle');
  function closeMobile() {
    if (mobilePanel) mobilePanel.hidden = true;
    if (mobileToggle) mobileToggle.setAttribute('aria-expanded', 'false');
  }
  if (mobileToggle) mobileToggle.addEventListener('click', () => {
    mobilePanel.hidden = !mobilePanel.hidden;
    mobileToggle.setAttribute('aria-expanded', String(!mobilePanel.hidden));
  });
  const items = tocs.map(toc => Array.from(toc.querySelectorAll('a[href^="#"]')).map(link => {
    let id = link.hash.slice(1);
    try { id = decodeURIComponent(id); } catch (_) {}
    return {link, heading: document.getElementById(id)};
  }).filter(item => item.heading));
  let scheduled = false;
  function update() {
    scheduled = false;
    for (const group of items) {
      let current = group[0];
      for (const item of group) if (item.heading.getBoundingClientRect().top <= 100) current = item;
      for (const item of group) {
        if (item === current) item.link.setAttribute('aria-current', 'location');
        else item.link.removeAttribute('aria-current');
      }
    }
  }
  function schedule() { if (!scheduled) { scheduled = true; requestAnimationFrame(update); } }
  for (const group of items) for (const {link, heading} of group) {
    link.addEventListener('click', () => {
      closeMobile();
      requestAnimationFrame(() => { heading.tabIndex = -1; heading.focus({preventScroll: true}); schedule(); });
    });
  }
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && mobilePanel && !mobilePanel.hidden) { closeMobile(); mobileToggle.focus(); }
  });
  window.addEventListener('scroll', schedule, {passive: true});
  window.addEventListener('resize', () => { closeMobile(); schedule(); });
  window.addEventListener('load', schedule);
  if (typeof ResizeObserver !== 'undefined') new ResizeObserver(schedule).observe(content);
  schedule();
})();
