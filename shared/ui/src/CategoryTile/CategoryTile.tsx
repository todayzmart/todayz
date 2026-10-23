import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import type { Tint } from '../StoreCard/StoreCard';

export interface CategoryTileProps {
  label: ReactNode;
  icon: LucideIcon;
  tint: Tint;
  href: string;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** A pastel category shortcut (tz-cat): 56px tinted square, label below. */
export function CategoryTile({ label, icon, tint, href, LinkComponent, className }: CategoryTileProps) {
  const Link = LinkComponent ?? 'a';
  return (
    <Link href={href} className={cx('tz-cat', className)} data-tint={tint}>
      <i>
        <Icon icon={icon} />
      </i>
      {label}
    </Link>
  );
}

export interface CategoryRowProps {
  /** Accessible name of the row, e.g. "Categories". */
  label: string;
  categories: readonly (CategoryTileProps & { key: string })[];
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Horizontally scrolling row of category tiles (tz-cats). */
export function CategoryRow({ label, categories, LinkComponent, className }: CategoryRowProps) {
  return (
    <nav className={cx('tz-cats', className)} aria-label={label}>
      {categories.map(({ key, ...c }) => (
        <CategoryTile key={key} LinkComponent={LinkComponent} {...c} />
      ))}
    </nav>
  );
}
