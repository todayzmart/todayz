import { sitemapXml } from '../lib/seo.ts';
import { SITE_LOCALES } from '../lib/site-locales.ts';

export function GET(): Response {
  return new Response(sitemapXml(SITE_LOCALES), { headers: { 'Content-Type': 'application/xml; charset=utf-8' } });
}
