import { AdminProductImageSource } from '@todayz/api-client/gen/model/adminProductImageSource';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { IMAGE_BASE_URL } from '../app/env';

export const IMAGE_SOURCES = Object.values(AdminProductImageSource);

/** The image host URL for an approved `imageKey`, or undefined when there is none. */
export function imageUrl(key: string | null | undefined): string | undefined {
  if (!key || IMAGE_BASE_URL === '') return undefined;
  return `${IMAGE_BASE_URL}/${key.split('/').map(encodeURIComponent).join('/')}`;
}

/** Two letters for the placeholder, as customers see it. */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const first = words[0] ?? '';
  const second = words[1] ?? '';
  const out = words.length > 1 ? `${[...first][0] ?? ''}${[...second][0] ?? ''}` : [...first].slice(0, 2).join('');
  return out.toUpperCase();
}

/** Where an image came from and how the importer matched it (D-252). */
export function useImageLabels() {
  const intl = useIntl();
  return useMemo(() => {
    const source = (s: AdminProductImageSource): string => intl.formatMessage({ id: `catalogue.images.source.${s}` });
    const match = (m: string | null): string | null => {
      if (m === 'barcode') return intl.formatMessage({ id: 'catalogue.images.match.barcode' });
      if (m === 'name') return intl.formatMessage({ id: 'catalogue.images.match.name' });
      return null;
    };
    return { source, match };
  }, [intl]);
}
