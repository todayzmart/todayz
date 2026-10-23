// JSON-LD builders (Organization, WebSite, SoftwareApplication, FAQPage). Plain objects, tested.
import { LOCALE_INFO, type Locale } from '../i18n/index.ts';
import { SITE_ORIGIN } from './pages.ts';

interface CompanyFacts {
  name: string;
  email: string | null;
  sameAs: readonly string[];
}

export function organizationJsonLd(company: CompanyFacts): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    name: company.name,
    url: SITE_ORIGIN,
    logo: `${SITE_ORIGIN}/icons/icon-512.png`,
    ...(company.email ? { email: company.email } : {}),
    ...(company.sameAs.length > 0 ? { sameAs: [...company.sameAs] } : {}),
  };
}

export function websiteJsonLd(locale: Locale): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'WebSite',
    name: 'TodayZ',
    url: SITE_ORIGIN,
    inLanguage: LOCALE_INFO[locale].tag,
  };
}

/** The shop-side app, free for now (pricing page). */
export function softwareApplicationJsonLd(locale: Locale, description: string): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: 'TodayZ for shops',
    applicationCategory: 'BusinessApplication',
    operatingSystem: 'Web',
    description,
    inLanguage: LOCALE_INFO[locale].tag,
    offers: { '@type': 'Offer', price: '0', priceCurrency: 'INR' },
  };
}

export function faqJsonLd(items: readonly { q: string; a: string }[]): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: items.map((item) => ({
      '@type': 'Question',
      name: item.q,
      acceptedAnswer: { '@type': 'Answer', text: item.a },
    })),
  };
}
