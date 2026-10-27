// The item's photo beside a line in the cart, checkout and an order: a small pastel square the photo
// fills, or the item's initials when it has none.
import { imageUrl, initials } from '../home/present';
import { tintOf } from '../store/itemUi';
import styles from './tx.module.css';

export function LineThumb({ name, imageKey, src, tintKey }: { name: string; imageKey?: string | null | undefined; src?: string | undefined; tintKey: string }) {
  const url = imageUrl(imageKey) ?? src;
  return (
    <span className={styles.thumb} data-tint={tintOf(tintKey)} aria-hidden="true">
      {url ? <img src={url} alt="" loading="lazy" decoding="async" /> : initials(name)}
    </span>
  );
}
