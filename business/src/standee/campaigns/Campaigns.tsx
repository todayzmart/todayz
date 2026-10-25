import { ApiError } from '@todayz/api-client';
import type { Standee } from '@todayz/api-client/gen/model/standee';
import { newUlid } from '@todayz/auth';
import { Banner, Button, EmptyState, Skeleton, StatusPill, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Plus, QrCode, RefreshCw } from 'lucide-react';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../../orders/store';
import { Funnel } from '../analytics/Funnel';
import styles from '../standee.module.css';
import { addStandee, reorderStandee, standeeErrorKey, useStandees } from './queries';

/** The contract's StandeeCampaign bound (ck_qr_standees_campaign). */
const CAMPAIGN_MAX = 40;

/**
 * Standee campaigns (B-10, W3-09): one row per printed standee, each with its own QR link
 * (`?s=<standee id>`, D-26), its scans → sign-ups → qualified counts and a reorder request.
 */
export function Campaigns({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const list = useStandees(store);

  return (
    <section className="tz-card" aria-labelledby="sc-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="sc-title">
          {intl.formatMessage({ id: 'standee.campaigns.title' })}
        </h2>
      </div>
      <div className={styles.stack}>
        <p className={styles.sub}>{intl.formatMessage({ id: 'standee.campaigns.body' })}</p>
        {list.isPending && (
          <div role="status" aria-label={intl.formatMessage({ id: 'standee.campaigns.loading' })}>
            <Skeleton height="6rem" />
          </div>
        )}
        {list.isError && (
          <Banner tone="warning" action={<Button onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'standee.retry' })}</Button>}>
            {intl.formatMessage({ id: 'standee.campaigns.loadError' })}
          </Banner>
        )}
        {list.data && (
          <>
            {list.data.items.length > 0 && <Funnel counts={list.data.totals} label={intl.formatMessage({ id: 'standee.funnel.totals' })} />}
            <NewCampaign store={store} full={list.data.items.length >= list.data.limit} limit={list.data.limit} />
            {list.data.items.length === 0 ? (
              <EmptyState icon={QrCode} title={intl.formatMessage({ id: 'standee.campaigns.empty.title' })} description={intl.formatMessage({ id: 'standee.campaigns.empty.body' })} />
            ) : (
              <ul className={styles.list}>
                {list.data.items.map((s) => (
                  <Row key={s.id} store={store} standee={s} />
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    </section>
  );
}

function NewCampaign({ store, full, limit }: { store: CurrentStore; full: boolean; limit: number }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const [campaign, setCampaign] = useState('');
  const [showError, setShowError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const key = useRef<{ sig: string; id: string } | null>(null);
  const name = campaign.trim();
  const err = name === '' ? 'required' : [...name].length > CAMPAIGN_MAX ? 'max' : null;

  const submit = async () => {
    setShowError(true);
    if (err || full) return;
    key.current = key.current?.sig === name ? key.current : { sig: name, id: newUlid() };
    setBusy(true);
    setFailure(null);
    try {
      await addStandee(qc, store, name, key.current.id);
      toast.show({ message: intl.formatMessage({ id: 'standee.campaigns.created' }) });
      setCampaign('');
      setShowError(false);
      key.current = null;
    } catch (e) {
      // Any answer from the server retires the key; a network failure keeps it so a retry cannot double-post.
      if (e instanceof ApiError) key.current = null;
      setFailure(e);
    } finally {
      setBusy(false);
    }
  };

  if (full) return <Banner tone="info">{intl.formatMessage({ id: 'standee.campaigns.full' }, { limit })}</Banner>;
  return (
    <form
      className={styles.newRow}
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <TextField
        label={intl.formatMessage({ id: 'standee.campaigns.name' })}
        help={intl.formatMessage({ id: 'standee.campaigns.nameHelp' }, { max: CAMPAIGN_MAX })}
        error={showError && err ? intl.formatMessage({ id: `standee.campaigns.err.${err}` }, { max: CAMPAIGN_MAX }) : undefined}
        value={campaign}
        onChange={(e) => setCampaign(e.target.value)}
      />
      {failure !== null && <Banner tone="warning">{intl.formatMessage({ id: `standee.campaigns.error.${standeeErrorKey(failure)}` }, { limit })}</Banner>}
      <div className={styles.actions}>
        <Button type="submit" variant="primary" icon={Plus} loading={busy} loadingLabel={intl.formatMessage({ id: 'standee.campaigns.adding' })}>
          {intl.formatMessage({ id: 'standee.campaigns.add' })}
        </Button>
      </div>
    </form>
  );
}

function Row({ store, standee }: { store: CurrentStore; standee: Standee }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const key = useRef<string | null>(null);
  const pending = standee.reorderRequestedAt !== null;
  const date = (iso: string) => intl.formatDate(iso, { day: 'numeric', month: 'short' });

  const reorder = async () => {
    key.current ??= newUlid();
    setBusy(true);
    setFailure(null);
    try {
      await reorderStandee(qc, store, standee.id, key.current);
      key.current = null;
    } catch (e) {
      if (e instanceof ApiError) key.current = null;
      setFailure(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <li className={styles.item}>
      <div className={styles.itemHead}>
        <strong>{standee.campaign}</strong>
        {pending && <StatusPill tone="pending">{intl.formatMessage({ id: 'standee.campaigns.reorderPending' })}</StatusPill>}
      </div>
      <p className={styles.sub}>
        {standee.lastScanAt
          ? intl.formatMessage({ id: 'standee.campaigns.lastScan' }, { date: date(standee.lastScanAt) })
          : intl.formatMessage({ id: 'standee.campaigns.noScans' })}
      </p>
      <Funnel counts={standee.counts} label={intl.formatMessage({ id: 'standee.funnel.one' }, { campaign: standee.campaign })} />
      {failure !== null && <Banner tone="warning">{intl.formatMessage({ id: `standee.campaigns.error.${standeeErrorKey(failure)}` }, { limit: 0 })}</Banner>}
      {pending ? (
        <p className={styles.sub}>{intl.formatMessage({ id: 'standee.campaigns.reorderSince' }, { date: date(standee.reorderRequestedAt ?? standee.createdAt) })}</p>
      ) : (
        <div className={styles.actions}>
          <Button size="sm" variant="soft" icon={RefreshCw} loading={busy} loadingLabel={intl.formatMessage({ id: 'standee.campaigns.reordering' })} onClick={() => void reorder()}>
            {intl.formatMessage({ id: 'standee.campaigns.reorder' })}
          </Button>
        </div>
      )}
    </li>
  );
}
