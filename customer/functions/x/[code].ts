// `/x/{code}` share links (D-101, 02 §4): link previews need Open Graph tags in the HTML, which a
// client-rendered app cannot give. This Pages Function reads GET /v1/public/share-cards/{code} and
// returns a small page with the tags that then opens C-08 with `ref=share`. On any failure (410,
// 404, the API down) it falls through to the app, whose `/x/$code` route shows the right state.
// Without `API_URL` it logs and redirects instead (onRequestGet), so a missing setting is seen.
// Locally the same `renderShare` runs as Vite dev middleware (vite.config.ts). Never a distance.

export interface SharePreviewLike {
  storeItemId: string;
  itemName: string;
  storeName: string;
  storeSlug: string;
  locality: string | null;
  pricePaise: number;
  imageUrl: string;
}

const CODE = /^[0-9A-Za-z]{10}$/;

export function itemPath(p: Pick<SharePreviewLike, 'storeSlug' | 'storeItemId'>): string {
  return `/s/${encodeURIComponent(p.storeSlug)}/i/${encodeURIComponent(p.storeItemId)}?ref=share`;
}

function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

/** ₹ with Indian grouping and no paise when whole, the same as the app's formatMoney. */
function rupees(paise: number): string {
  const r = paise / 100;
  return `₹${r.toLocaleString('en-IN', { minimumFractionDigits: Number.isInteger(r) ? 0 : 2, maximumFractionDigits: 2 })}`;
}

export function shareHtml(p: SharePreviewLike, origin: string, code: string): string {
  const to = itemPath(p);
  const title = `${p.itemName} at ${p.storeName}`;
  const desc = [rupees(p.pricePaise), p.locality].filter(Boolean).join(' · ') + ' · TodayZ';
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} · TodayZ</title>
<meta name="robots" content="noindex">
<meta property="og:type" content="website">
<meta property="og:site_name" content="TodayZ">
<meta property="og:title" content="${esc(title)}">
<meta property="og:description" content="${esc(desc)}">
<meta property="og:image" content="${esc(p.imageUrl)}">
<meta property="og:url" content="${esc(`${origin}/x/${code}`)}">
<meta name="twitter:card" content="summary_large_image">
<meta http-equiv="refresh" content="0;url=${esc(to)}">
</head>
<body><a href="${esc(to)}">${esc(title)}</a></body>
</html>`;
}

/**
 * The preview page for `code`, or null to fall through to the app. `apiBase` is the API origin
 * (no trailing slash).
 */
export async function renderShare(code: string, origin: string, apiBase: string, fetcher: typeof fetch = fetch): Promise<Response | null> {
  if (!CODE.test(code) || !apiBase) return null;
  try {
    const r = await fetcher(`${apiBase}/v1/public/share-cards/${code}`, { headers: { accept: 'application/json' } });
    if (!r.ok) return null;
    const p = (await r.json()) as SharePreviewLike;
    if (!p.storeSlug || !p.storeItemId) return null;
    return new Response(shareHtml(p, origin, code), {
      headers: { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'public, max-age=300', 'x-robots-tag': 'noindex' },
    });
  } catch {
    return null;
  }
}

interface PagesContext {
  request: Request;
  params: { code?: string | string[] };
  env: { API_URL?: string };
  next: () => Promise<Response>;
}

/** The query flag on the app's own copy of the link: the function lets it through to the app. */
export const APP_FLAG = 'app';

/**
 * Without `API_URL` no link can have a preview, so the function says so: it logs an error and answers
 * a plain 302 to the same link with `?app=1`, which it lets through to the app's `/x/$code` route (that
 * still resolves the code). The 302 shows in the Pages logs and analytics, where a silent fall-through
 * would not. The lead sets `API_URL` (https://api.todayz.in) on the Pages project.
 */
export async function onRequestGet(ctx: PagesContext): Promise<Response> {
  const code = typeof ctx.params.code === 'string' ? ctx.params.code : '';
  const url = new URL(ctx.request.url);
  if (url.searchParams.has(APP_FLAG) || !CODE.test(code)) return ctx.next();
  const apiBase = (ctx.env.API_URL ?? '').replace(/\/$/, '');
  if (!apiBase) {
    console.error('share link: API_URL is not set on the Pages project; no preview, sending the link to the app');
    return new Response(null, { status: 302, headers: { location: `/x/${code}?${APP_FLAG}=1`, 'cache-control': 'no-store' } });
  }
  return (await renderShare(code, url.origin, apiBase)) ?? ctx.next();
}
