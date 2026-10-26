import { type ReactNode, useState } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import type { Tint } from '../StoreCard/StoreCard';

export interface CategoryTileProps {
  label: ReactNode;
  icon: LucideIcon;
  tint: Tint;
  href: string;
  /** A photo in the tile's place (customer Home, D-249); the tinted icon shows while it is missing or fails to load. */
  imageUrl?: string | undefined;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** A pastel category shortcut (tz-cat): 56px tinted square, label below. */
export function CategoryTile({ label, icon, tint, href, imageUrl, LinkComponent, className }: CategoryTileProps) {
  const Link = LinkComponent ?? 'a';
  const [failed, setFailed] = useState<string | undefined>();
  const photo = imageUrl !== undefined && imageUrl !== failed ? imageUrl : undefined;
  return (
    <Link href={href} className={cx('tz-cat', className)} data-tint={tint}>
      <i>
        {photo ? <img src={photo} alt="" loading="lazy" decoding="async" onError={() => setFailed(photo)} /> : <Icon icon={icon} />}
      </i>
      {label}
    </Link>
  );
}

export interface CategoryRowProps {
  /** Accessible name of the row, e.g. "Categories". */
  label: string;
  categories: readonly (CategoryTileProps & { key: string })[];
  /** row: one scrolling line. grid: three to a line, for round photos (customer Home, D-249). */
  layout?: 'row' | 'grid' | undefined;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Horizontally scrolling row of category tiles (tz-cats), or a grid of them. */
export function CategoryRow({ label, categories, layout = 'row', LinkComponent, className }: CategoryRowProps) {
  return (
    <nav className={cx('tz-cats', layout === 'grid' && 'tz-cats-grid', className)} aria-label={label}>
      {categories.map(({ key, ...c }) => (
        <CategoryTile key={key} LinkComponent={LinkComponent} {...c} />
      ))}
    </nav>
  );
}
