import type { AdminBanner } from '@todayz/api-client/gen/model/adminBanner';
import type { AdminBannerState } from '@todayz/api-client/gen/model/adminBannerState';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

/** Banners a person may take down: not already off. */
export const canTakeDown = (b: Pick<AdminBanner, 'status'>) => b.status !== 'removed' && b.status !== 'ended';
/** A held upload waiting for a person. */
export const waiting = (b: Pick<AdminBanner, 'status' | 'moderation'>) => b.status === 'pending_review' && b.moderation === 'needs_review';

export function bannerTone(b: Pick<AdminBanner, 'status'>): PillTone {
  switch (b.status) {
    case 'pending_review':
      return 'pending';
    case 'live':
      return 'completed';
    case 'removed':
      return 'cancelled';
    default:
      return 'neutral';
  }
}

export function useBannerLabels() {
  const intl = useIntl();
  return useMemo(() => {
    return {
      state: (s: AdminBannerState): string => {
        switch (s) {
          case 'live':
            return intl.formatMessage({ id: 'banners.tab.live' });
          case 'removed':
            return intl.formatMessage({ id: 'banners.tab.removed' });
          default:
            return intl.formatMessage({ id: 'banners.tab.review' });
        }
      },
      status: (b: Pick<AdminBanner, 'status'>): string => {
        switch (b.status) {
          case 'pending_review':
            return intl.formatMessage({ id: 'banners.status.pending' });
          case 'live':
            return intl.formatMessage({ id: 'banners.status.live' });
          case 'scheduled':
            return intl.formatMessage({ id: 'banners.status.scheduled' });
          case 'paused':
            return intl.formatMessage({ id: 'banners.status.paused' });
          case 'ended':
            return intl.formatMessage({ id: 'banners.status.ended' });
          default:
            return intl.formatMessage({ id: 'banners.status.removed' });
        }
      },
      target: (b: Pick<AdminBanner, 'targetType' | 'targetLabel' | 'targetItemCount'>): string => {
        switch (b.targetType) {
          case 'offer':
            return intl.formatMessage({ id: 'banners.target.offer' }, { name: b.targetLabel ?? '' });
          case 'category':
            return intl.formatMessage({ id: 'banners.target.category' }, { name: b.targetLabel ?? '' });
          case 'items':
            return intl.formatMessage({ id: 'banners.target.items' }, { n: b.targetItemCount });
          default:
            return intl.formatMessage({ id: 'banners.target.store' });
        }
      },
      kind: (b: Pick<AdminBanner, 'imageKey'>): string =>
        b.imageKey ? intl.formatMessage({ id: 'banners.kind.upload' }) : intl.formatMessage({ id: 'banners.kind.template' }),
    };
  }, [intl]);
}
