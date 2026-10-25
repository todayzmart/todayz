import type { AdminBanner } from '@todayz/api-client/gen/model/adminBanner';
import { IMAGE_BASE_URL } from '../app/env';
import css from './banners.module.css';

/** The banner as shoppers would see it: the shop's photo, or its template on the chosen tint. */
export function BannerPreview({ b }: { b: Pick<AdminBanner, 'headline' | 'imageKey' | 'template'> }) {
  if (b.imageKey && IMAGE_BASE_URL) {
    return (
      <div className={css.preview}>
        <img src={`${IMAGE_BASE_URL}/${b.imageKey}`} alt={b.headline} loading="lazy" />
      </div>
    );
  }
  return (
    <div className={css.preview} data-tint={b.template?.tint}>
      <p className={css.headline}>{b.headline}</p>
      {b.template?.subline && <p className={css.subline}>{b.template.subline}</p>}
    </div>
  );
}
