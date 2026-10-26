import { ChevronRight } from 'lucide-react';
import { type ReactNode, useState } from 'react';
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
  /** The shop's photo; shown in the avatar's place, with the initials while it is missing or fails to load. */
  imageUrl?: string | undefined;
  /** First meta line, pre-formatted: distance, area, matches ("0.6 km", "Jayanagar 4th Block"). */
  meta: readonly ReactNode[];
  tags?: readonly StoreTag[] | undefined;
  href: string;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Shop row for search results and the map sheet (tz-scard); the whole card is one link. */
export function StoreCard({ name, initials, tint, imageUrl, meta, tags = [], href, LinkComponent, className }: StoreCardProps) {
  const Link = LinkComponent ?? 'a';
  const [failed, setFailed] = useState<string | undefined>();
  const photo = imageUrl !== undefined && imageUrl !== failed ? imageUrl : undefined;
  return (
    <Link href={href} className={cx('tz-scard', 'tz-ui-plainlink', className)}>
      <div className={cx('tz-avatar', photo && 'tz-ui-avatar-photo')} data-tint={tint} aria-hidden="true">
        {photo ? (
          <img className="tz-ui-thumb-img" src={photo} alt="" loading="lazy" decoding="async" onError={() => setFailed(photo)} />
        ) : (
          initials
        )}
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
