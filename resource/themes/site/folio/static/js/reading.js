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

  const toc = document.querySelector('.reader-toc');
  const content = document.querySelector('[data-reading-content]');
  if (!toc || !content) return;
  const details = toc.querySelector('details');
  const links = Array.from(toc.querySelectorAll('a[href^="#"]'));
  function headingFor(link) {
    let id = link.hash.slice(1);
    try { id = decodeURIComponent(id); } catch (_) { /* A literal percent is valid in a heading ID. */ }
    return document.getElementById(id);
  }
  const items = links.map(link => ({ link, heading: headingFor(link) })).filter(item => item.heading);
  const wide = window.matchMedia('(min-width: 1440px)');
  const progress = toc.querySelector('progress');
  const label = toc.querySelector('.reader-progress-label');
  let scheduled = false;
  function update() {
    scheduled = false;
    const rect = content.getBoundingClientRect();
    const offset = parseFloat(getComputedStyle(toc).getPropertyValue('--reader-offset')) || 96;
    const percent = Math.round(Math.max(0, Math.min(1, (offset - rect.top) / Math.max(1, rect.height - window.innerHeight + offset))) * 100);
    progress.value = percent;
    label.textContent = percent + '%';
    toc.classList.toggle('reader-toc-finished', wide.matches && rect.bottom < offset);
    let current = items[0];
    for (const item of items) {
      if (item.heading.getBoundingClientRect().top <= offset + 40) current = item;
    }
    for (const item of items) {
      if (item === current) item.link.setAttribute('aria-current', 'location');
      else item.link.removeAttribute('aria-current');
    }
  }
  function schedule() { if (!scheduled) { scheduled = true; requestAnimationFrame(update); } }
  function resize() { details.open = wide.matches; schedule(); }
  wide.addEventListener('change', resize);
  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', schedule);
  window.addEventListener('load', schedule);
  if (typeof ResizeObserver !== 'undefined') new ResizeObserver(schedule).observe(content);
  links.forEach(link => link.addEventListener('click', () => {
    if (!wide.matches) details.open = false;
    const heading = headingFor(link);
    if (heading) requestAnimationFrame(() => { heading.tabIndex = -1; heading.focus({ preventScroll: true }); schedule(); });
  }));
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && details.open && !wide.matches) {
      details.open = false;
      toc.querySelector('summary').focus();
    }
  });
  resize();
})();
