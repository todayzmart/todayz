import { ChevronRight, Star } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';
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
  /** row: avatar, text and chevron (search, map sheet). feature: the photo across the top, then the text (customer Home, D-249). */
  layout?: 'row' | 'feature' | undefined;
  /** feature only: a tag on the photo's top-left corner (open, or closed in black). */
  photoTag?: StoreTag | undefined;
  /** feature only: a big line at the photo's foot over a dark scrim, e.g. "5% cashback" / "as credits". */
  highlight?: { title: ReactNode; sub?: ReactNode | undefined } | undefined;
  /** feature only: the green badge beside the name. `label` is what a screen reader hears ("Rated 4.6 of 5"). */
  rating?: { value: string; label: string } | undefined;
  /** feature only: a line under a dashed rule. offer: info blue (cashback); plain: ink-2. */
  footer?: { label: ReactNode; icon?: LucideIcon | undefined; kind?: 'offer' | 'plain' | undefined } | undefined;
  className?: string | undefined;
}

/** Shop row for search results and the map sheet (tz-scard); the whole card is one link. */
export function StoreCard({
  name,
  initials,
  tint,
  imageUrl,
  meta,
  tags = [],
  href,
  LinkComponent,
  layout = 'row',
  photoTag,
  highlight,
  rating,
  footer,
  className,
}: StoreCardProps) {
  const Link = LinkComponent ?? 'a';
  const [failed, setFailed] = useState<string | undefined>();
  const photo = imageUrl !== undefined && imageUrl !== failed ? imageUrl : undefined;
  const picture = photo ? (
    <img className="tz-ui-thumb-img" src={photo} alt="" loading="lazy" decoding="async" onError={() => setFailed(photo)} />
  ) : (
    initials
  );
  const nameLine =
    layout === 'feature' && rating ? (
      <div className="tz-scard-r1">
        <div className="tz-scard-name">{name}</div>
        <span className="tz-scard-rate">
          <span aria-hidden="true">{rating.value}</span>
          <Star className="tz-ico" strokeWidth={1.5} aria-hidden="true" focusable="false" />
          <span className="tz-ui-sr-only">{rating.label}</span>
        </span>
      </div>
    ) : (
      <div className="tz-scard-name">{name}</div>
    );
  const text = (
    <div className={layout === 'feature' ? 'tz-scard-body' : 'tz-ui-minw0'}>
      {nameLine}
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
  );
  if (layout === 'feature') {
    return (
      <Link href={href} className={cx('tz-scard', 'tz-scard-feature', 'tz-ui-plainlink', className)}>
        {/* The name comes first for a screen reader; the photo's tag and highlight follow it. */}
        {text}
        <div className={cx('tz-scard-photo', photo && 'tz-ui-avatar-photo')} data-tint={tint}>
          {photo ? picture : <span aria-hidden="true">{initials}</span>}
          {photoTag && (
            <span className="tz-scard-ptag" data-kind={photoTag.kind ?? 'plain'}>
              {photoTag.label}
            </span>
          )}
          {highlight && (
            <span className="tz-scard-hl">
              <b>{highlight.title}</b>
              {highlight.sub && <span>{highlight.sub}</span>}
            </span>
          )}
        </div>
        {footer && (
          <div className="tz-scard-foot" data-kind={footer.kind ?? 'plain'}>
            {footer.icon && <Icon icon={footer.icon} />}
            <span>{footer.label}</span>
          </div>
        )}
      </Link>
    );
  }
  return (
    <Link href={href} className={cx('tz-scard', 'tz-ui-plainlink', className)}>
      <div className={cx('tz-avatar', photo && 'tz-ui-avatar-photo')} data-tint={tint} aria-hidden="true">
        {picture}
      </div>
      {text}
      <span className="tz-ui-chevron">
        <Icon icon={ChevronRight} />
      </span>
    </Link>
  );
}
