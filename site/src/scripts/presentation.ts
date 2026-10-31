const root = document.documentElement;
const media = matchMedia('(prefers-color-scheme: dark)');
const toggle = document.querySelector<HTMLButtonElement>('[data-theme-toggle]');
let explicit = false;
try { explicit = ['light', 'dark'].includes(localStorage.getItem('todayz-site-theme') ?? ''); } catch {}
function apply(theme: 'light' | 'dark'): void {
  root.dataset['theme'] = theme;
  document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', theme);
  toggle?.setAttribute('aria-pressed', String(theme === 'dark'));
}
if (toggle) {
  toggle.hidden = false;
  toggle.setAttribute('aria-pressed', String(root.dataset['theme'] === 'dark'));
  toggle.addEventListener('click', () => {
    const theme = root.dataset['theme'] === 'dark' ? 'light' : 'dark';
    explicit = true;
    try { localStorage.setItem('todayz-site-theme', theme); } catch {}
    apply(theme);
  });
}
media.addEventListener('change', (event) => { if (!explicit) apply(event.matches ? 'dark' : 'light'); });

// Reflect the pointer as light across the glass; never run an idle animation loop.
const reduced = matchMedia('(prefers-reduced-motion: reduce)');
const pointer = matchMedia('(hover: hover) and (pointer: fine)');
document.querySelectorAll<HTMLElement>('.p-discovery').forEach((panel) => {
  let frame = 0;
  panel.addEventListener('pointermove', (event) => {
    if (reduced.matches || !pointer.matches) return;
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      const rect = panel.getBoundingClientRect();
      const x = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width));
      const y = Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height));
      panel.style.setProperty('--glass-x', `${x * 100}%`);
      panel.style.setProperty('--glass-y', `${y * 100}%`);
      panel.style.setProperty('--glass-rx', `${(0.5 - y) * 1.6}deg`);
      panel.style.setProperty('--glass-ry', `${(x - 0.5) * 1.6}deg`);
    });
  });
  panel.addEventListener('pointerleave', () => {
    cancelAnimationFrame(frame);
    ['--glass-x', '--glass-y', '--glass-rx', '--glass-ry'].forEach((name) => panel.style.removeProperty(name));
  });
});
