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
