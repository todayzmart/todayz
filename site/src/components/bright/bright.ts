// Client behaviour of the bright landing (D-262), bundled by Astro as one same-origin script. Every
// part is optional: the HTML reads right without it (finished walk, full rings, first shop, every
// shelf item, starting savings). Motion is skipped under prefers-reduced-motion.
import { savings, inr } from './data.ts';

const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
document.documentElement.classList.add('b-js');

/* Journey: the green line and walker follow the route once, then "Play the walk again". */
function journey(root: HTMLElement): () => void {
  const route = root.querySelector<SVGPathElement>('[data-route]');
  const done = root.querySelector<SVGPathElement>('[data-route-done]');
  const walkers = root.querySelectorAll<SVGCircleElement>('[data-walker]');
  const metres = root.querySelector('[data-count="metres"]');
  const minutes = root.querySelector('[data-count="minutes"]');
  const replay = root.querySelector<HTMLButtonElement>('[data-journey-replay]');
  if (!route || !done) return () => undefined;
  const total = route.getTotalLength();
  const M = Number(root.dataset['metres'] ?? 0);
  const MIN = Number(root.dataset['minutes'] ?? 0);
  done.style.strokeDasharray = String(total);
  const set = (p: number) => {
    const pt = route.getPointAtLength(p * total);
    walkers.forEach((w) => {
      w.setAttribute('cx', String(pt.x));
      w.setAttribute('cy', String(pt.y));
    });
    done.style.strokeDashoffset = String(total * (1 - p));
    if (metres) metres.textContent = String(Math.round(p * M));
    if (minutes) minutes.textContent = String(Math.round(p * MIN));
  };
  let raf = 0;
  const play = () => {
    cancelAnimationFrame(raf);
    if (reduce) return set(1);
    let t0: number | undefined;
    const step = (ts: number) => {
      t0 ??= ts;
      const p = Math.min(1, (ts - t0) / 8000);
      set(p);
      if (p < 1) raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
  };
  set(reduce ? 1 : 0);
  if (replay) {
    replay.hidden = false;
    replay.addEventListener('click', play);
  }
  return play;
}

/* Orbit: the shop dots become buttons that show their card. */
function orbit(root: HTMLElement): void {
  const cards = root.querySelectorAll<HTMLElement>('[data-card]');
  const show = (n: string) => cards.forEach((c) => (c.hidden = c.dataset['card'] !== n));
  root.querySelectorAll<SVGGElement>('[data-node]').forEach((node) => {
    const n = node.dataset['node'] ?? '0';
    node.setAttribute('role', 'button');
    node.setAttribute('tabindex', '0');
    node.setAttribute('aria-label', node.dataset['label'] ?? '');
    node.addEventListener('mouseenter', () => show(n));
    node.addEventListener('click', () => show(n));
    node.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        show(n);
      }
    });
  });
}

/* Shelf: category buttons filter the items. */
function shelf(tabs: HTMLElement, list: HTMLElement): void {
  tabs.hidden = false;
  tabs.addEventListener('click', (e) => {
    const btn = (e.target as Element).closest<HTMLButtonElement>('button[data-cat]');
    if (!btn) return;
    const cat = btn.dataset['cat'];
    tabs.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', String(b === btn)));
    list.querySelectorAll<HTMLElement>('[data-cat]').forEach((item) => (item.hidden = cat !== 'all' && item.dataset['cat'] !== cat));
  });
}

/* Savings: recompute the year on every slider move. */
function savingsCalc(root: HTMLElement): void {
  const input = (id: string) => root.querySelector<HTMLInputElement>(`input[name="${id}"]`);
  const ids = ['orders', 'fee', 'basket', 'cashback', 'walk'];
  const res = (k: string) => root.querySelector<HTMLElement>(`[data-res="${k}"]`);
  const fill = (el: HTMLElement | null, v: string) => {
    if (el) el.textContent = (el.dataset['fmt'] ?? '{v}').replace('{v}', v);
  };
  const update = () => {
    const [o, f, b, c, w] = ids.map((id) => Number(input(id)?.value ?? 0)) as [number, number, number, number, number];
    for (const id of ids) {
      const el = input(id);
      const out = root.querySelector<HTMLElement>(`[data-out="${id}"]`);
      if (!el || !out) continue;
      const v = Number(el.value);
      const shown = id === 'walk' ? v.toFixed(1) : id === 'basket' ? inr(v) : String(v);
      fill(out, shown);
      el.setAttribute('aria-valuetext', out.textContent ?? shown);
    }
    const r = savings(o, f, b, c, w);
    fill(res('total'), inr(r.total));
    fill(res('fees'), inr(r.fees));
    fill(res('credits'), inr(r.credits));
    fill(res('km'), inr(r.km));
    fill(res('hours'), r.hours.toFixed(1));
  };
  root.addEventListener('input', update);
}

const playWalk = (() => {
  const root = document.querySelector<HTMLElement>('[data-journey]');
  return root ? journey(root) : () => undefined;
})();
document.querySelectorAll<HTMLElement>('[data-orbit]').forEach(orbit);
const tabs = document.querySelector<HTMLElement>('[data-shelf-tabs]');
const list = document.querySelector<HTMLElement>('[data-shelf]');
if (tabs && list) shelf(tabs, list);
document.querySelectorAll<HTMLElement>('[data-savings]').forEach(savingsCalc);

/* Reveal on scroll; the journey plays the first time it is seen. */
const io = new IntersectionObserver(
  (entries) =>
    entries.forEach((e) => {
      if (!e.isIntersecting) return;
      e.target.classList.add('in');
      if (e.target.hasAttribute('data-journey')) playWalk();
      io.unobserve(e.target);
    }),
  { threshold: 0.15 },
);
document.querySelectorAll('.bp .rv').forEach((el) => (reduce ? el.classList.add('in') : io.observe(el)));
