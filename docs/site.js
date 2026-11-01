// Moontop site: install tabs, copy buttons, setup viewer, screenshot viewer.
(() => {
  // install tabs
  document.querySelectorAll('[data-tabs]').forEach((box) => {
    const tabs = [...box.querySelectorAll('[role=tab]')];
    const select = (tab) => {
      tabs.forEach((t) => {
        const on = t === tab;
        t.setAttribute('aria-selected', on);
        t.tabIndex = on ? 0 : -1;
        document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
      });
    };
    tabs.forEach((t, i) => {
      t.addEventListener('click', () => select(t));
      t.addEventListener('keydown', (e) => {
        const d = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
        if (!d) return;
        const next = tabs[(i + d + tabs.length) % tabs.length];
        select(next);
        next.focus();
      });
    });
    // default to the platform's natural choice
    const ua = navigator.userAgent;
    if (/Windows/.test(ua)) select(box.querySelector('#t-scoop'));
    else if (/Linux|X11|FreeBSD/.test(ua) && !/Android/.test(ua)) select(box.querySelector('#t-curl'));
  });

  // copy buttons (copy the command, not the prompt)
  document.querySelectorAll('.copy').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const code = btn.parentElement.querySelector('code').cloneNode(true);
      code.querySelectorAll('.ps1').forEach((p) => p.remove());
      const text = code.textContent.split('\n').map((l) => l.trim()).join('\n').trim();
      try {
        await navigator.clipboard.writeText(text);
        btn.textContent = 'copied';
        btn.classList.add('done');
      } catch {
        btn.textContent = 'select + copy';
      }
      setTimeout(() => { btn.textContent = 'copy'; btn.classList.remove('done'); }, 1600);
    });
  });

  // setup viewer
  const setup = document.querySelector('[data-setup]');
  if (setup) {
    const img = document.getElementById('setup-img');
    const link = document.getElementById('setup-link');
    const note = document.getElementById('setup-note');
    const btns = [...setup.querySelectorAll('[role=tab]')];
    const show = (b) => {
      btns.forEach((x) => x.setAttribute('aria-selected', x === b));
      const n = b.dataset.img;
      img.src = `shots/${n}-view.webp`;
      img.alt = `Setup screen, ${b.textContent} category`;
      link.href = `shots/${n}.webp`;
      link.dataset.caption = `Setup · ${b.textContent}`;
      note.textContent = b.dataset.note;
    };
    btns.forEach((b, i) => {
      b.addEventListener('click', () => show(b));
      b.addEventListener('keydown', (e) => {
        const d = e.key === 'ArrowDown' || e.key === 'ArrowRight' ? 1 : e.key === 'ArrowUp' || e.key === 'ArrowLeft' ? -1 : 0;
        if (!d) return;
        e.preventDefault();
        const next = btns[(i + d + btns.length) % btns.length];
        show(next);
        next.focus();
      });
    });
    // warm the cache so switching is instant
    btns.forEach((b) => { const i = new Image(); i.src = `shots/${b.dataset.img}-view.webp`; });
  }

  // screenshot viewer
  const dlg = document.getElementById('viewer');
  if (!dlg || typeof dlg.showModal !== 'function') return;
  const vImg = document.getElementById('viewer-img');
  const vCap = document.getElementById('viewer-caption');
  const vCount = document.getElementById('viewer-count');
  const vFull = document.getElementById('viewer-full');
  let shots = [];
  let idx = 0;
  const render = () => {
    const a = shots[idx];
    vImg.src = a.getAttribute('href');
    vImg.alt = a.querySelector('img')?.alt || '';
    vCap.textContent = a.dataset.caption || '';
    vCount.textContent = `${idx + 1}/${shots.length}`;
    vFull.href = a.getAttribute('href');
  };
  const step = (d) => { idx = (idx + d + shots.length) % shots.length; render(); };
  document.addEventListener('click', (e) => {
    const a = e.target.closest('[data-shot]');
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey) return;
    e.preventDefault();
    shots = [...document.querySelectorAll('[data-shot]')];
    idx = shots.indexOf(a);
    render();
    dlg.showModal();
  });
  dlg.querySelectorAll('[data-step]').forEach((b) => b.addEventListener('click', () => step(+b.dataset.step)));
  document.getElementById('viewer-close').addEventListener('click', () => dlg.close());
  dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
  dlg.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowRight' || e.key === 'l') step(1);
    else if (e.key === 'ArrowLeft' || e.key === 'h') step(-1);
    else if (e.key === 'q') dlg.close();
  });
  dlg.addEventListener('close', () => { vImg.removeAttribute('src'); });
})();
