import { Banner, Button, Skeleton, useToast } from '@todayz/ui';
import { Copy, Share2 } from 'lucide-react';
import { useIntl } from 'react-intl';
import type { BusinessReferrals } from './api';
import styles from './credits.module.css';

/**
 * Copies to the clipboard; resolves false when the browser refuses. The text is never logged,
 * stored or sent anywhere.
 */
async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

interface Props {
  referrals: BusinessReferrals | undefined;
  loading: boolean;
  failed: boolean;
  retry: () => void;
}

/** The business referral code (shop-to-shop only, D-26), a share link and the stats. */
export function ReferralCard({ referrals, loading, failed, retry }: Props) {
  const intl = useIntl();
  const toast = useToast();
  const canShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function';

  const copy = async (text: string, ok: string) => {
    const done = await copyText(text);
    toast.show({ message: intl.formatMessage({ id: done ? ok : 'credits.referral.copyFailed' }) });
  };
  const share = async (link: string) => {
    try {
      await navigator.share({ title: intl.formatMessage({ id: 'credits.referral.shareTitle' }), url: link });
    } catch {
      // The person closed the share sheet; nothing to do.
    }
  };

  return (
    <section className="tz-card" aria-labelledby="cr-ref-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="cr-ref-title">
          {intl.formatMessage({ id: 'credits.referral.title' })}
        </h2>
      </div>
      {loading && <Skeleton height="5rem" />}
      {failed && (
        <Banner tone="warning" action={<Button onClick={retry}>{intl.formatMessage({ id: 'credits.retry' })}</Button>}>
          {intl.formatMessage({ id: 'credits.referral.error' })}
        </Banner>
      )}
      {referrals && (
        <div className={styles.stack}>
          <p className={styles.sub}>{intl.formatMessage({ id: 'credits.referral.body' })}</p>
          <div className={styles.codeRow}>
            <span className={styles.code} aria-label={intl.formatMessage({ id: 'credits.referral.codeLabel' })}>
              {referrals.code}
            </span>
            <Button icon={Copy} onClick={() => void copy(referrals.code, 'credits.referral.copied')}>
              {intl.formatMessage({ id: 'credits.referral.copy' })}
            </Button>
          </div>
          <div className={styles.actions}>
            <Button icon={Copy} onClick={() => void copy(referrals.link, 'credits.referral.linkCopied')}>
              {intl.formatMessage({ id: 'credits.referral.copyLink' })}
            </Button>
            {canShare && (
              <Button icon={Share2} onClick={() => void share(referrals.link)}>
                {intl.formatMessage({ id: 'credits.referral.share' })}
              </Button>
            )}
          </div>
          <dl className={styles.stats}>
            <div>
              <dt>{intl.formatMessage({ id: 'credits.referral.pending' })}</dt>
              <dd>{intl.formatMessage({ id: 'credits.referral.count' }, { count: referrals.stats.pending, credits: referrals.stats.pendingCredits })}</dd>
            </div>
            <div>
              <dt>{intl.formatMessage({ id: 'credits.referral.rewarded' })}</dt>
              <dd>{intl.formatMessage({ id: 'credits.referral.count' }, { count: referrals.stats.rewarded, credits: referrals.stats.rewardedCredits })}</dd>
            </div>
          </dl>
          <p className={styles.sub}>{intl.formatMessage({ id: 'credits.referral.where' })}</p>
        </div>
      )}
    </section>
  );
}
