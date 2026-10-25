import type { AdminBanner } from '@todayz/api-client/gen/model/adminBanner';
import type { AdminBannerState } from '@todayz/api-client/gen/model/adminBannerState';
import { Button, DataTable, StatusPill, Tabs, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Ban, Check } from 'lucide-react';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { BannerPreview } from './BannerPreview';
import { bannerTone, canTakeDown, useBannerLabels, waiting } from './labels';
import { useBanners, useDecide, type Decision } from './queries';
import { STATES, type BannersSearch } from './search';
import css from '../app/page.module.css';

type Go = (next: BannersSearch) => void;

/** A-20 Banners: uploads the automatic check held, live banners and taken-down ones; clear or take down with a reason. */
export function BannersPage({ search }: { search: BannersSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const labels = useBannerLabels();
  const go: Go = (next) => void navigate({ to: '/banners', search: next });
  const state = search.state ?? 'review';
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.banners' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'banners.sub' })}</p>
      </div>
      <Tabs
        label={intl.formatMessage({ id: 'banners.tabs' })}
        tabs={STATES.map((s) => ({ value: s, label: labels.state(s) }))}
        value={state}
        onChange={(v) => go({ ...(v === 'review' ? {} : { state: v as 'live' | 'removed' }), ...(search.storeId ? { storeId: search.storeId } : {}) })}
      />
      <BannerList search={search} go={go} />
    </div>
  );
}

function BannerList({ search, go }: { search: BannersSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useBannerLabels();
  const problemText = useProblemText();
  const state: AdminBannerState = search.state ?? 'review';
  const filter = useMemo(() => ({ state, ...(search.storeId ? { storeId: search.storeId } : {}) }), [state, search.storeId]);
  const q = useBanners(filter);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const selected = search.banner ? rows.find((b) => b.id === search.banner) : undefined;

  const columns = useMemo<ColumnDef<AdminBanner, unknown>[]>(
    () => [
      {
        id: 'banner',
        header: intl.formatMessage({ id: 'banners.col.banner' }),
        cell: ({ row }) => (
          <Button size="sm" variant="ghost" aria-pressed={row.original.id === search.banner} onClick={() => go({ ...search, banner: row.original.id })}>
            {row.original.headline}
          </Button>
        ),
      },
      {
        id: 'store',
        header: intl.formatMessage({ id: 'banners.col.store' }),
        accessorFn: (b) => (b.storeCity ? `${b.storeName}, ${b.storeCity}` : b.storeName),
      },
      { id: 'target', header: intl.formatMessage({ id: 'banners.col.target' }), accessorFn: (b) => labels.target(b) },
      state === 'removed'
        ? { id: 'reason', header: intl.formatMessage({ id: 'banners.col.reason' }), accessorFn: (b) => b.removeReason ?? '' }
        : { id: 'kind', header: intl.formatMessage({ id: 'banners.col.kind' }), accessorFn: (b) => labels.kind(b) },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'banners.col.status' }),
        cell: ({ row }) => <StatusPill tone={bannerTone(row.original)}>{labels.status(row.original)}</StatusPill>,
      },
      {
        id: 'when',
        header: intl.formatMessage({ id: state === 'removed' ? 'banners.col.removed' : 'banners.col.uploaded' }),
        accessorFn: (b) => when(state === 'removed' && b.removedAt ? b.removedAt : b.createdAt, now ?? b.createdAt),
      },
    ],
    [intl, labels, when, now, state, search, go],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <StoreFilter key={search.storeId ?? ''} search={search} onApply={go} />
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={labels.state(state)}
            columns={columns}
            data={rows}
            getRowId={(b) => b.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: state === 'review' ? 'banners.empty.review' : 'banners.empty' })}
            footer={
              q.hasNextPage ? (
                <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                  {intl.formatMessage({ id: 'common.loadMore' })}
                </Button>
              ) : undefined
            }
          />
        )}
      </div>
      <div className={css.column}>
        {selected ? (
          <BannerCard key={selected.id} b={selected} now={now ?? selected.createdAt} />
        ) : (
          <p className={`tz-card ${css.muted}`}>
            {intl.formatMessage({ id: search.banner && !q.isPending ? 'banners.pick.notListed' : 'banners.pick' })}
          </p>
        )}
      </div>
    </div>
  );
}

function StoreFilter({ search, onApply }: { search: BannersSearch; onApply: Go }) {
  const intl = useIntl();
  const [storeId, setStoreId] = useState(search.storeId ?? '');
  const keep = search.state ? { state: search.state } : {};
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const id = storeId.trim().toUpperCase();
    onApply({ ...keep, ...(id ? { storeId: id } : {}) });
  };
  return (
    <form className={css.filters} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'banners.filters' })}>
      <TextField label={intl.formatMessage({ id: 'banners.filter.store' })} value={storeId} onChange={(e) => setStoreId(e.target.value)} autoComplete="off" />
      <div className={css.actions}>
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'banners.filter.apply' })}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onApply(keep)}>
          {intl.formatMessage({ id: 'banners.filter.clear' })}
        </Button>
      </div>
    </form>
  );
}

/** The chosen banner: its preview, where it points and when it runs, the moves the caller may make, and its history. */
function BannerCard({ b, now }: { b: AdminBanner; now: string }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useBannerLabels();
  const problemText = useProblemText();
  const decide = useDecide(b.id);
  const [asking, setAsking] = useState<Decision['kind'] | null>(null);
  const may = can(roles, 'bannerModerate');
  const open = (k: Decision['kind']) => {
    decide.reset();
    setAsking(k);
  };
  const run = (d: Decision, done: string) =>
    decide.mutate(d, {
      onSuccess: () => {
        setAsking(null);
        toast.show({ message: done });
      },
    });

  return (
    <>
      <section className="tz-card" aria-labelledby={`banner-${b.id}`}>
        <h2 id={`banner-${b.id}`} className="tz-card-title">
          {intl.formatMessage({ id: 'banners.detail.title' }, { store: b.storeName })}
        </h2>
        <BannerPreview b={b} />
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'banners.detail.headline' })}</dt>
          <dd>{b.headline}</dd>
          <dt>{intl.formatMessage({ id: 'banners.col.kind' })}</dt>
          <dd>{labels.kind(b)}</dd>
          <dt>{intl.formatMessage({ id: 'banners.col.target' })}</dt>
          <dd>{labels.target(b)}</dd>
          <dt>{intl.formatMessage({ id: 'banners.detail.starts' })}</dt>
          <dd>{when(b.startsAt, now)}</dd>
          <dt>{intl.formatMessage({ id: 'banners.detail.ends' })}</dt>
          <dd>{b.endsAt ? when(b.endsAt, now) : intl.formatMessage({ id: 'banners.detail.noEnd' })}</dd>
          <dt>{intl.formatMessage({ id: 'banners.col.status' })}</dt>
          <dd>
            <StatusPill tone={bannerTone(b)}>{labels.status(b)}</StatusPill>
            {b.removeReason && <p className={css.muted}>{intl.formatMessage({ id: 'banners.detail.why' }, { reason: b.removeReason })}</p>}
          </dd>
          <dt>{intl.formatMessage({ id: 'banners.col.uploaded' })}</dt>
          <dd>{when(b.createdAt, now)}</dd>
        </dl>
        {may && (waiting(b) || canTakeDown(b)) && (
          <div className={css.actions}>
            {waiting(b) && (
              <Button variant="primary" icon={Check} onClick={() => open('approve')}>
                {intl.formatMessage({ id: 'banners.approve' })}
              </Button>
            )}
            {canTakeDown(b) && (
              <Button variant="danger" icon={Ban} onClick={() => open('takedown')}>
                {intl.formatMessage({ id: 'banners.takedown' })}
              </Button>
            )}
          </div>
        )}
      </section>
      <AuditPanel type="banner" id={b.id} />
      <ReasonDialog
        open={asking === 'approve'}
        title={intl.formatMessage({ id: 'banners.approveTitle' }, { store: b.storeName })}
        description={intl.formatMessage({ id: 'banners.approveBody' })}
        confirmLabel={intl.formatMessage({ id: 'banners.approve' })}
        label={intl.formatMessage({ id: 'banners.note' })}
        help={intl.formatMessage({ id: 'banners.noteHelp' })}
        optional
        pending={decide.isPending}
        error={decide.error ? problemText(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'approve', ...(reason ? { reason } : {}) }, intl.formatMessage({ id: 'banners.approved' }))}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking === 'takedown'}
        title={intl.formatMessage({ id: 'banners.takedownTitle' }, { store: b.storeName })}
        description={intl.formatMessage({ id: 'banners.takedownBody' })}
        confirmLabel={intl.formatMessage({ id: 'banners.takedown' })}
        danger
        pending={decide.isPending}
        error={decide.error ? problemText(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'takedown', reason }, intl.formatMessage({ id: 'banners.takenDown' }))}
        onClose={() => setAsking(null)}
      />
    </>
  );
}
