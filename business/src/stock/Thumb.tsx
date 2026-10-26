import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { IMAGE_BASE_URL } from '../app/env';
import styles from './stock.module.css';

/** The image URL for an approved `imageKey`, or undefined when there is none or no image host. */
function imageUrl(key: string | null | undefined): string | undefined {
  if (!key || IMAGE_BASE_URL === '') return undefined;
  return `${IMAGE_BASE_URL}/${key.split('/').map(encodeURIComponent).join('/')}`;
}

/** Two letters for the placeholder. */
function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const first = words[0] ?? '';
  const second = words[1] ?? '';
  const out = words.length > 1 ? `${[...first][0] ?? ''}${[...second][0] ?? ''}` : [...first].slice(0, 2).join('');
  return out.toUpperCase();
}

/**
 * The product's picture (D-252): the approved image, else the store's own photo while it waits
 * for review, else the initials. A turned-down photo is not shown.
 */
export function Thumb({ item, large = false }: { item: Pick<StoreItem, 'name' | 'imageKey' | 'photo'>; large?: boolean }) {
  const src = imageUrl(item.imageKey) ?? (item.photo?.status === 'pending' ? item.photo.previewUrl : undefined);
  return (
    <span className={large ? `${styles.thumb} ${styles.thumbLarge}` : styles.thumb} aria-hidden="true">
      {src ? <img src={src} alt="" loading="lazy" decoding="async" /> : initials(item.name)}
    </span>
  );
}
