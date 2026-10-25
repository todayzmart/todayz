import type { AdminReview } from '@todayz/api-client/gen/model/adminReview';
import type { AdminReviewReport } from '@todayz/api-client/gen/model/adminReviewReport';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import type { Move } from './queries';

/** How much of a review's text the list shows before it is cut. */
const SNIPPET = 60;

export function useReviewLabels() {
  const intl = useIntl();
  return useMemo(() => {
    return {
      snippet: (text: string | null | undefined): string => {
        if (!text) return intl.formatMessage({ id: 'reviews.noText' });
        return text.length > SNIPPET ? `${text.slice(0, SNIPPET).trimEnd()}…` : text;
      },
      stars: (n: number): string => intl.formatMessage({ id: 'reviews.stars' }, { n }),
      tone: (r: Pick<AdminReview, 'hidden' | 'openReports'>): PillTone => (r.hidden ? 'cancelled' : r.openReports > 0 ? 'pending' : 'completed'),
      state: (r: Pick<AdminReview, 'hidden' | 'openReports'>): string =>
        r.hidden
          ? intl.formatMessage({ id: 'reviews.state.hidden' })
          : r.openReports > 0
            ? intl.formatMessage({ id: 'reviews.state.reported' })
            : intl.formatMessage({ id: 'reviews.state.visible' }),
      filterState: (s: string): string => {
        switch (s) {
          case 'reported':
            return intl.formatMessage({ id: 'reviews.filter.reported' });
          case 'hidden':
            return intl.formatMessage({ id: 'reviews.filter.hidden' });
          default:
            return intl.formatMessage({ id: 'reviews.filter.visible' });
        }
      },
      reporter: (k: string): string =>
        k === 'store' ? intl.formatMessage({ id: 'reviews.reporter.store' }) : intl.formatMessage({ id: 'reviews.reporter.customer' }),
      reportFilter: (s: string): string =>
        s === 'resolved' ? intl.formatMessage({ id: 'reviews.reports.resolved' }) : intl.formatMessage({ id: 'reviews.reports.open' }),
      reportStatus: (r: Pick<AdminReviewReport, 'status' | 'resolution'>): string => {
        if (r.status === 'open') return intl.formatMessage({ id: 'reviews.reports.open' });
        return r.resolution === 'upheld'
          ? intl.formatMessage({ id: 'reviews.resolution.upheld' })
          : intl.formatMessage({ id: 'reviews.resolution.dismissed' });
      },
      moveTitle: (mv: Move, store: string): string => {
        switch (mv) {
          case 'hide':
            return intl.formatMessage({ id: 'reviews.move.hideTitle' }, { store });
          case 'restore':
            return intl.formatMessage({ id: 'reviews.move.restoreTitle' }, { store });
          case 'replyHide':
            return intl.formatMessage({ id: 'reviews.move.replyHideTitle' }, { store });
          case 'replyRestore':
            return intl.formatMessage({ id: 'reviews.move.replyRestoreTitle' }, { store });
        }
      },
      moveBody: (mv: Move): string => {
        switch (mv) {
          case 'hide':
            return intl.formatMessage({ id: 'reviews.move.hideBody' });
          case 'restore':
            return intl.formatMessage({ id: 'reviews.move.restoreBody' });
          case 'replyHide':
            return intl.formatMessage({ id: 'reviews.move.replyHideBody' });
          case 'replyRestore':
            return intl.formatMessage({ id: 'reviews.move.replyRestoreBody' });
        }
      },
      moveConfirm: (mv: Move): string => {
        switch (mv) {
          case 'hide':
            return intl.formatMessage({ id: 'reviews.move.hide' });
          case 'restore':
            return intl.formatMessage({ id: 'reviews.move.restore' });
          case 'replyHide':
            return intl.formatMessage({ id: 'reviews.move.replyHide' });
          case 'replyRestore':
            return intl.formatMessage({ id: 'reviews.move.replyRestore' });
        }
      },
      moveDone: (mv: Move): string => {
        switch (mv) {
          case 'hide':
            return intl.formatMessage({ id: 'reviews.move.hideDone' });
          case 'restore':
            return intl.formatMessage({ id: 'reviews.move.restoreDone' });
          case 'replyHide':
            return intl.formatMessage({ id: 'reviews.move.replyHideDone' });
          case 'replyRestore':
            return intl.formatMessage({ id: 'reviews.move.replyRestoreDone' });
        }
      },
    };
  }, [intl]);
}
