import { ChevronRight } from 'lucide-react';
import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';

export type Tint = 'green' | 'amber' | 'rose' | 'sky' | 'lilac' | 'peach';

export interface StoreTag {
  label: ReactNode;
  /** open: green dot ("Open till 10 pm"); offer: ink text; plain: muted (closed, pickup, delivery). */
  kind?: 'open' | 'offer' | 'plain' | undefined;
}

export interface StoreCardProps {
  name: string;
  /** Two letters for the avatar, e.g. "SL". */
  initials: string;
  tint?: Tint | undefined;
  /** First meta line, pre-formatted: distance, area, matches ("0.6 km", "Jayanagar 4th Block"). */
  meta: readonly ReactNode[];
  tags?: readonly StoreTag[] | undefined;
  href: string;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Shop row for search results and the map sheet (tz-scard); the whole card is one link. */
export function StoreCard({ name, initials, tint, meta, tags = [], href, LinkComponent, className }: StoreCardProps) {
  const Link = LinkComponent ?? 'a';
  return (
    <Link href={href} className={cx('tz-scard', 'tz-ui-plainlink', className)}>
      <div className="tz-avatar" data-tint={tint} aria-hidden="true">
        {initials}
      </div>
      <div className="tz-ui-minw0">
        <div className="tz-scard-name">{name}</div>
        <div className="tz-meta">
          {meta.map((m, i) => (
            <span key={i}>{m}</span>
          ))}
        </div>
        {tags.length > 0 && (
          <div className="tz-meta tz-ui-meta-tags">
            {tags.map((t, i) => (
              <span
                key={i}
                className={cx('tz-tag', t.kind === 'open' && 'tz-tag-open', t.kind === 'offer' && 'tz-tag-offer')}
              >
                {t.label}
              </span>
            ))}
          </div>
        )}
      </div>
      <span className="tz-ui-chevron">
        <Icon icon={ChevronRight} />
      </span>
    </Link>
  );
}
