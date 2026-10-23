import { sitemapXml } from '../lib/seo.ts';

export function GET(): Response {
  return new Response(sitemapXml(), { headers: { 'Content-Type': 'application/xml; charset=utf-8' } });
}
