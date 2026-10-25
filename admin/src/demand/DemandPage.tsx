import type { DemandGap } from '@todayz/api-client/gen/model/demandGap';
import type { MarqueeCity } from '@todayz/api-client/gen/model/marqueeCity';
import { Button, DataTable, EmptyState, Skeleton, StatusPill, Tabs, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Download, Megaphone } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { saveCsv } from '../payouts/queries';
import { gapsCsv } from './csv';
import { canApproveStrip } from './perm';
import { useDecide, useGaps, useMarquee } from './queries';
import type { DemandSearch } from './search';
import css from '../app/page.module.css';
import own from './demand.module.css';

type Go = (next: DemandSearch) => void;

/**
 * A-23 Demand (D-101). Gaps: per city, what customers asked shops for and no shop said yes to,
 * read-only and never naming a customer. Site strip: the nightly proposal for the site's "What
 * people near you are searching for" strip; leave products out, then Approve (audited).
 */
export function DemandPage({ search }: { search: DemandSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const strip = canApproveStrip(useRoles());
  const tab = search.tab === 'strip' && strip ? 'strip' : 'gaps';
  const go: Go = (next) => void navigate({ to: '/demand', search: next });
  const city = search.city ? { city: search.city } : {};
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.demand' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'demand.sub' })}</p>
      </div>
      {strip && (
        <Tabs
          label={intl.formatMessage({ id: 'demand.tabs' })}
          tabs={[
            { value: 'gaps', label: intl.formatMessage({ id: 'demand.tab.gaps' }) },
            { value: 'strip', label: intl.formatMessage({ id: 'demand.tab.strip' }) },
          ]}
          value={tab}
          onChange={(v) => go({ ...(v === 'strip' ? { tab: 'strip' } : {}), ...city })}
        />
      )}
      {tab === 'gaps' ? <Gaps search={search} go={go} /> : <Strip search={search} go={go} />}
    </div>
  );
}

function CityPicker({ id, cities, value, onChange }: { id: string; cities: readonly { id: string; name: string }[]; value?: string | undefined; onChange: (city?: string) => void }) {
  const intl = useIntl();
  return (
    <div className="tz-field">
      <label className="tz-field-label" htmlFor={id}>
        {intl.formatMessage({ id: 'demand.city' })}
      </label>
      <select id={id} className="tz-input" value={value ?? ''} onChange={(e) => onChange(e.target.value || undefined)}>
        <option value="">{intl.formatMessage({ id: 'demand.city.all' })}</option>
        {cities.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
    </div>
  );
}

function Gaps({ search, go }: { search: DemandSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const problemText = useProblemText();
  const q = useGaps();
  const all = useMemo(() => q.data?.items ?? [], [q.data]);
  const now = q.data?.serverNow;
  const cities = useMemo(() => {
    const seen = new Map<string, string>();
    for (const g of all) seen.set(g.cityId, g.cityName);
    return [...seen].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name));
  }, [all]);
  const rows = search.city ? all.filter((g) => g.cityId === search.city) : all;
  const none = intl.formatMessage({ id: 'demand.none' });

  const columns = useMemo<ColumnDef<DemandGap, unknown>[]>(
    () => [
      {
        id: 'product',
        header: intl.formatMessage({ id: 'demand.col.product' }),
        cell: ({ row }) => (
          <span className={own.name}>
            {row.original.itemName}
            {row.original.productId === null && <StatusPill tone="neutral">{intl.formatMessage({ id: 'demand.gaps.unmatched' })}</StatusPill>}
          </span>
        ),
      },
      ...(search.city ? [] : [{ id: 'city', header: intl.formatMessage({ id: 'demand.col.city' }), accessorFn: (g: DemandGap) => g.cityName }]),
      { id: 'category', header: intl.formatMessage({ id: 'demand.col.category' }), accessorFn: (g) => g.category ?? none },
      { id: 'customers', header: intl.formatMessage({ id: 'demand.col.customers' }), accessorFn: (g) => g.customers, meta: { numeric: true } },
      { id: 'asks', header: intl.formatMessage({ id: 'demand.col.asks' }), accessorFn: (g) => g.asks, meta: { numeric: true } },
      { id: 'last', header: intl.formatMessage({ id: 'demand.col.last' }), accessorFn: (g) => when(g.lastAskedAt, now ?? g.lastAskedAt) },
    ],
    [intl, when, now, none, search.city],
  );

  const exportCsv = () => {
    const slug = search.city ? (cities.find((c) => c.id === search.city)?.name ?? 'city').toLowerCase().replace(/[^a-z0-9]+/g, '-') : 'all';
    saveCsv(`todayz-demand-gaps-${slug}-${(now ?? '').slice(0, 10)}.csv`, gapsCsv(rows));
  };

  if (q.isError) {
    return (
      <p role="alert" className="tz-field-error">
        {problemText(q.error)}{' '}
        <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'common.retry' })}
        </Button>
      </p>
    );
  }
  return (
    <div className={css.column}>
      <div className={css.head}>
        <form className={css.form} role="search" aria-label={intl.formatMessage({ id: 'demand.filters' })} onSubmit={(e) => e.preventDefault()}>
          <CityPicker id="demand-gaps-city" cities={cities} value={search.city} onChange={(city) => go(city ? { city } : {})} />
        </form>
        <Button icon={Download} disabled={rows.length === 0} onClick={exportCsv}>
          {intl.formatMessage({ id: 'demand.gaps.export' })}
        </Button>
      </div>
      {q.data && <p className={css.muted}>{intl.formatMessage({ id: 'demand.gaps.rule' }, { min: q.data.minCustomers, days: q.data.days })}</p>}
      <DataTable
        label={intl.formatMessage({ id: 'demand.gaps.table' })}
        columns={columns}
        data={rows}
        getRowId={(g) => `${g.cityId}:${g.productId ?? g.itemName}`}
        loading={q.isPending}
        emptyLabel={intl.formatMessage({ id: 'demand.gaps.empty' })}
      />
    </div>
  );
}

function Strip({ search, go }: { search: DemandSearch; go: Go }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useMarquee(true);
  const all = q.data?.cities ?? [];
  const cities = all.map((c) => ({ id: c.cityId, name: c.cityName }));
  const shown = search.city ? all.filter((c) => c.cityId === search.city) : all;

  if (q.isError) {
    return (
      <p role="alert" className="tz-field-error">
        {problemText(q.error)}{' '}
        <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'common.retry' })}
        </Button>
      </p>
    );
  }
  if (!q.data) return <Skeleton shape="block" height="320px" />;
  return (
    <div className={css.column}>
      <form className={css.form} role="search" aria-label={intl.formatMessage({ id: 'demand.filters' })} onSubmit={(e) => e.preventDefault()}>
        <CityPicker id="demand-strip-city" cities={cities} value={search.city} onChange={(city) => go({ tab: 'strip', ...(city ? { city } : {}) })} />
      </form>
      <p className={css.muted}>{intl.formatMessage({ id: 'demand.strip.rule' }, { min: q.data.minSearchers })}</p>
      {shown.length === 0 ? (
        <EmptyState icon={Megaphone} title={intl.formatMessage({ id: 'demand.strip.empty' })} />
      ) : (
        shown.map((c) => <CityStrip key={`${c.cityId}:${c.latest?.builtOn ?? ''}:${c.latest?.status ?? ''}`} city={c} now={q.data.serverNow} />)
      )}
    </div>
  );
}

// TODO(lead): approvals are audited as subject type `site_marquee`, which the admin-audit contract's
// AdminAuditSubjectType does not list yet, so the shared AuditPanel cannot show them here. The card
// shows the approved list and its date instead; A-19 lists the `marquee.decide` entries.
function CityStrip({ city, now }: { city: MarqueeCity; now: string }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const problemText = useProblemText();
  const decide = useDecide(city.cityId);
  const proposal = city.latest?.status === 'proposed' ? city.latest : null;
  const [left, setLeft] = useState<ReadonlySet<string>>(() => new Set());
  const keep = proposal ? proposal.items.filter((i) => !left.has(i.productId)) : [];
  const approved = city.approved;

  const approve = () => {
    if (!proposal || keep.length === 0) return;
    decide.mutate(
      { builtOn: proposal.builtOn, decision: 'approve', productIds: keep.map((i) => i.productId) },
      {
        onSuccess: () => toast.show({ message: intl.formatMessage({ id: 'demand.strip.done' }, { city: city.cityName, n: keep.length }) }),
        onError: (e) => toast.show({ message: problemText(e) }),
      },
    );
  };

  return (
    <section className={`tz-card ${own.city}`} aria-labelledby={`strip-${city.cityId}`}>
      <h2 id={`strip-${city.cityId}`} className="tz-card-title">
        {city.cityName}
      </h2>
      {proposal ? (
        <fieldset className={own.proposal}>
          <legend className={own.legend}>{intl.formatMessage({ id: 'demand.strip.proposed' }, { day: proposal.builtOn })}</legend>
          <p className={css.muted}>{intl.formatMessage({ id: 'demand.strip.leaveOut' })}</p>
          <ol className={own.items}>
            {proposal.items.map((i) => (
              <li key={i.productId}>
                <label className={own.item}>
                  <input
                    type="checkbox"
                    checked={!left.has(i.productId)}
                    onChange={(e) =>
                      setLeft((prev) => {
                        const next = new Set(prev);
                        if (e.target.checked) next.delete(i.productId);
                        else next.add(i.productId);
                        return next;
                      })
                    }
                  />
                  <span className={own.name}>{i.productName}</span>
                  <span className={css.muted}>
                    {intl.formatMessage({ id: 'demand.strip.searchers' }, { n: i.searchers })}
                    {i.category ? ` · ${i.category}` : ''}
                  </span>
                </label>
              </li>
            ))}
          </ol>
          <div className={css.actions}>
            <Button variant="primary" disabled={keep.length === 0} loading={decide.isPending} onClick={approve}>
              {intl.formatMessage({ id: 'demand.strip.approve' }, { n: keep.length })}
            </Button>
          </div>
        </fieldset>
      ) : (
        <p className={css.muted}>{intl.formatMessage({ id: 'demand.strip.noProposal' })}</p>
      )}
      {approved ? (
        <div className={own.approved}>
          <h3 className={own.legend}>
            {intl.formatMessage(
              { id: 'demand.strip.approvedOn' },
              { day: approved.builtOn, when: approved.decidedAt ? when(approved.decidedAt, now) : approved.builtOn },
            )}
          </h3>
          <ol className={own.chips}>
            {approved.items
              .filter((i) => i.status === 'approved')
              .map((i) => (
                <li key={i.productId} className="tz-chip">
                  {i.productName}
                </li>
              ))}
          </ol>
        </div>
      ) : (
        <p className={css.muted}>{intl.formatMessage({ id: 'demand.strip.notApproved' })}</p>
      )}
    </section>
  );
}
