// A tiny same-origin blocking asset applies the preference before the body paints.
(() => {
  let saved;
  try { saved = localStorage.getItem('todayz-site-theme'); } catch {}
  const theme = saved === 'light' || saved === 'dark' ? saved : matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', theme);
})();
