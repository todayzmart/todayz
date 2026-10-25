import type { AdminHoursRange } from '@todayz/api-client/gen/model/adminHoursRange';
import type { AdminStore } from '@todayz/api-client/gen/model/adminStore';
import type { AdminStoreStrike } from '@todayz/api-client/gen/model/adminStoreStrike';
import type { AdminStoreWeek } from '@todayz/api-client/gen/model/adminStoreWeek';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, EmptyState, IconButton, Skeleton, StatusPill, useToast, type ColumnDef } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { BadgeCheck, DoorClosed, DoorOpen, MapPinOff, Pencil, Plus, RotateCcw, Trash2, Undo2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { istInstant } from '../app/search';
import { ReasonDialog } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { WEEKDAYS, storeTone, useStoreLabels, type Weekday } from './labels';
import { useStore, useStoreChange, type StoreChange } from './queries';
import css from '../app/page.module.css';

/** At most this many opening ranges a day (the business hours rule the API checks). */
const MAX_RANGES = 4;

/** A-05 store detail: address, hours, reliability, strikes; edit, force-close, reopen, review, strike reset. */
export function StorePage({ id }: { id: string }) {
  const intl = useIntl();
  const q = useStore(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === 'not_found';
    return (
      <EmptyState
        icon={MapPinOff}
        headingLevel={2}
        title={intl.formatMessage({ id: missing ? 'stores.missing' : 'stores.loadFailed' })}
        action={
          missing ? (
            <Link to="/stores" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'stores.back' })}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>
          )
        }
      />
    );
  }
  return <Detail s={q.data} />;
}

type Open = 'edit' | 'forceClose' | 'reopen' | 'resetStrikes' | 'approve' | 'sendBack';

function Detail({ s }: { s: AdminStore }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useStoreLabels();
  const problemText = useProblemText();
  const change = useStoreChange(s.id);
  const [open, setOpen] = useState<Open | null>(null);
  const [until, setUntil] = useState('');
  const mayEdit = can(roles, 'storeEdit');
  const mayReview = can(roles, 'storeReview');
  const mayReset = can(roles, 'strikesReset');
  const now = s.serverNow;
  const closed = s.closedUntil !== null && Date.parse(s.closedUntil) > Date.parse(now);

  const close = () => {
    setOpen(null);
    setUntil('');
    change.reset();
  };
  const run = (c: StoreChange, done: string) =>
    change.mutate(c, {
      onSuccess: () => {
        close();
        toast.show({ message: intl.formatMessage({ id: done }) });
      },
    });

  const strikeCols = useMemo<ColumnDef<AdminStoreStrike, unknown>[]>(
    () => [
      { id: 'reason', header: intl.formatMessage({ id: 'stores.col.reason' }), accessorFn: (k) => labels.strike(k.reason) },
      {
        id: 'order',
        header: intl.formatMessage({ id: 'stores.col.order' }),
        cell: ({ row }) =>
          row.original.orderId ? (
            <Link to="/orders/$orderId" params={{ orderId: row.original.orderId }}>
              {intl.formatMessage({ id: 'stores.openOrder' })}
            </Link>
          ) : (
            '—'
          ),
      },
      { id: 'at', header: intl.formatMessage({ id: 'stores.col.when' }), accessorFn: (k) => when(k.createdAt, now) },
      {
        id: 'state',
        header: intl.formatMessage({ id: 'stores.col.state' }),
        cell: ({ row }) =>
          row.original.resetAt ? (
            <span className={css.muted}>
              {intl.formatMessage({ id: 'stores.strike.reset' }, { when: when(row.original.resetAt, now), reason: row.original.resetReason ?? 'none' })}
            </span>
          ) : (
            <StatusPill tone="pending">{intl.formatMessage({ id: 'stores.strike.active' })}</StatusPill>
          ),
      },
    ],
    [intl, labels, when, now],
  );

  const untilIso = istInstant(until);
  const untilBad = untilIso === null || Date.parse(untilIso) <= Date.parse(now);
  const error = change.isError ? problemText(change.error) : null;
  const address = [s.addressLine1, s.addressLine2, s.landmark, s.locality, s.city, s.pincode].filter(Boolean).join(', ');
  return (
    <div className={css.column}>
      <Link to="/stores" className={css.muted}>
        {intl.formatMessage({ id: 'stores.back' })}
      </Link>
      <div className={css.head}>
        <div>
          <div className={css.title}>
            <h1 className="tz-page-title">{s.name}</h1>
            <StatusPill tone={storeTone(s.status)}>{labels.status(s.status)}</StatusPill>
            {closed && s.closedUntil && (
              <StatusPill tone="cancelled">{intl.formatMessage({ id: 'stores.closedUntil' }, { when: when(s.closedUntil, now) })}</StatusPill>
            )}
          </div>
          <p className="tz-page-sub">
            <Link to="/businesses/$businessId" params={{ businessId: s.businessId }}>
              {s.businessName}
            </Link>{' '}
            · {address || '—'}
          </p>
        </div>
        <div className={css.actions}>
          {mayReview && s.status === 'pending_review' && (
            <>
              <Button variant="primary" icon={BadgeCheck} disabled={s.businessStatus !== 'verified'} onClick={() => setOpen('approve')}>
                {intl.formatMessage({ id: 'stores.approve' })}
              </Button>
              <Button icon={Undo2} onClick={() => setOpen('sendBack')}>
                {intl.formatMessage({ id: 'stores.sendBack' })}
              </Button>
            </>
          )}
          {mayEdit && (
            <Button icon={Pencil} onClick={() => setOpen('edit')}>
              {intl.formatMessage({ id: 'stores.edit' })}
            </Button>
          )}
          {mayEdit && !closed && (s.status === 'live' || s.status === 'paused') && (
            <Button variant="danger" icon={DoorClosed} onClick={() => setOpen('forceClose')}>
              {intl.formatMessage({ id: 'stores.forceClose' })}
            </Button>
          )}
          {mayEdit && closed && (
            <Button icon={DoorOpen} onClick={() => setOpen('reopen')}>
              {intl.formatMessage({ id: 'stores.reopen' })}
            </Button>
          )}
          {mayReset && (
            <Button icon={RotateCcw} disabled={s.activeStrikes === 0} onClick={() => setOpen('resetStrikes')}>
              {intl.formatMessage({ id: 'stores.resetStrikes' })}
            </Button>
          )}
        </div>
      </div>
      {mayReview && s.status === 'pending_review' && s.businessStatus !== 'verified' && (
        <p className={css.muted}>{intl.formatMessage({ id: 'stores.approve.needsBusiness' })}</p>
      )}
      {s.reviewNote && (
        <p className={css.muted}>{intl.formatMessage({ id: 'stores.reviewNote' }, { note: s.reviewNote })}</p>
      )}
      <div className={css.detail}>
        <div className={css.column}>
          <section className="tz-card" aria-labelledby="reliability-title">
            <h2 id="reliability-title" className="tz-card-title">
              {intl.formatMessage({ id: 'stores.reliability' })}
            </h2>
            <dl className={css.facts}>
              <dt>{intl.formatMessage({ id: 'stores.col.reliability' })}</dt>
              <dd className={css.mono}>{labels.percent(s.reliabilityScore)}</dd>
              <dt>{intl.formatMessage({ id: 'stores.acceptRate' })}</dt>
              <dd className={css.mono}>{labels.percent(s.acceptRate30d)}</dd>
              <dt>{intl.formatMessage({ id: 'stores.expiryRate' })}</dt>
              <dd className={css.mono}>{labels.percent(s.unacceptedExpiryRate30d)}</dd>
              <dt>{intl.formatMessage({ id: 'stores.rating' })}</dt>
              <dd className={css.mono}>
                {s.ratingAvg === null
                  ? '—'
                  : intl.formatMessage(
                      { id: 'stores.rating.value' },
                      { avg: intl.formatNumber(s.ratingAvg, { maximumFractionDigits: 1 }), count: s.ratingCount },
                    )}
              </dd>
              <dt>{intl.formatMessage({ id: 'stores.computedAt' })}</dt>
              <dd>{s.reliabilityComputedAt ? when(s.reliabilityComputedAt, now) : '—'}</dd>
            </dl>
          </section>

          <section className="tz-card" aria-labelledby="place-title">
            <h2 id="place-title" className="tz-card-title">
              {intl.formatMessage({ id: 'stores.place' })}
            </h2>
            <dl className={css.facts}>
              <dt>{intl.formatMessage({ id: 'stores.address' })}</dt>
              <dd>{address || '—'}</dd>
              <dt>{intl.formatMessage({ id: 'stores.pin' })}</dt>
              <dd className={css.mono}>
                {s.location
                  ? intl.formatMessage(
                      { id: 'stores.pin.value' },
                      {
                        lat: intl.formatNumber(s.location.lat, { maximumFractionDigits: 6 }),
                        lng: intl.formatNumber(s.location.lng, { maximumFractionDigits: 6 }),
                      },
                    )
                  : intl.formatMessage({ id: 'stores.pin.none' })}
              </dd>
              <dt>{intl.formatMessage({ id: 'stores.liveAt' })}</dt>
              <dd>{s.liveAt ? when(s.liveAt, now) : '—'}</dd>
            </dl>
          </section>

          <section className="tz-card" aria-labelledby="hours-title">
            <h2 id="hours-title" className="tz-card-title">
              {intl.formatMessage({ id: 'stores.hours' })}
            </h2>
            <dl className={css.facts}>
              {WEEKDAYS.map((d) => (
                <HoursRow key={d} day={d} ranges={s.hours.weekly[d] ?? []} />
              ))}
            </dl>
            {s.hours.holidays.length > 0 && (
              <p className={css.muted}>{intl.formatMessage({ id: 'stores.holidays' }, { count: s.hours.holidays.length })}</p>
            )}
          </section>

          <section className="tz-card" aria-labelledby="strikes-title">
            <h2 id="strikes-title" className="tz-card-title">
              {intl.formatMessage({ id: 'stores.strikes' }, { count: s.activeStrikes })}
            </h2>
            <DataTable
              label={intl.formatMessage({ id: 'stores.strikes.table' })}
              columns={strikeCols}
              data={s.strikes}
              getRowId={(k) => k.id}
              emptyLabel={intl.formatMessage({ id: 'stores.strikes.none' })}
            />
          </section>
        </div>
        <AuditPanel type="store" id={s.id} />
      </div>

      {open === 'edit' && <EditDialog s={s} change={change} error={error} close={close} run={run} />}
      <ReasonDialog
        open={open === 'forceClose'}
        title={intl.formatMessage({ id: 'stores.forceClose.title' }, { name: s.name })}
        description={intl.formatMessage({ id: 'stores.forceClose.body' })}
        confirmLabel={intl.formatMessage({ id: 'stores.forceClose' })}
        danger
        blocked={untilBad}
        pending={change.isPending}
        error={error}
        onConfirm={(reason) => untilIso && run({ kind: 'forceClose', reason, until: untilIso }, 'stores.done.forceClose')}
        onClose={close}
      >
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="force-close-until">
            {intl.formatMessage({ id: 'stores.forceClose.until' })}
          </label>
          <input
            id="force-close-until"
            className="tz-input"
            type="datetime-local"
            value={until}
            aria-invalid={until !== '' && untilBad}
            aria-describedby="force-close-until-help"
            onChange={(e) => setUntil(e.target.value)}
          />
          <p id="force-close-until-help" className={until !== '' && untilBad ? 'tz-field-error' : 'tz-field-help'}>
            {intl.formatMessage({ id: until !== '' && untilBad ? 'stores.forceClose.untilPast' : 'stores.forceClose.untilHelp' })}
          </p>
        </div>
      </ReasonDialog>
      <ReasonDialog
        open={open === 'reopen'}
        title={intl.formatMessage({ id: 'stores.reopen.title' }, { name: s.name })}
        description={intl.formatMessage({ id: 'stores.reopen.body' })}
        confirmLabel={intl.formatMessage({ id: 'stores.reopen' })}
        pending={change.isPending}
        error={error}
        onConfirm={(reason) => run({ kind: 'reopen', reason }, 'stores.done.reopen')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'resetStrikes'}
        title={intl.formatMessage({ id: 'stores.resetStrikes.title' }, { count: s.activeStrikes })}
        description={intl.formatMessage({ id: 'stores.resetStrikes.body' })}
        confirmLabel={intl.formatMessage({ id: 'stores.resetStrikes' })}
        pending={change.isPending}
        error={error}
        onConfirm={(reason) => run({ kind: 'resetStrikes', reason }, 'stores.done.resetStrikes')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'approve'}
        title={intl.formatMessage({ id: 'stores.approve.title' }, { name: s.name })}
        description={intl.formatMessage({ id: 'stores.approve.body' })}
        confirmLabel={intl.formatMessage({ id: 'stores.approve' })}
        label={intl.formatMessage({ id: 'stores.note.label' })}
        help={intl.formatMessage({ id: 'stores.note.optional' })}
        optional
        pending={change.isPending}
        error={error}
        onConfirm={(note) => run({ kind: 'review', decision: 'approve', note }, 'stores.done.approve')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'sendBack'}
        title={intl.formatMessage({ id: 'stores.sendBack.title' }, { name: s.name })}
        description={intl.formatMessage({ id: 'stores.sendBack.body' })}
        confirmLabel={intl.formatMessage({ id: 'stores.sendBack' })}
        label={intl.formatMessage({ id: 'stores.sendBack.note' })}
        help={intl.formatMessage({ id: 'stores.sendBack.noteHelp' })}
        pending={change.isPending}
        error={error}
        onConfirm={(note) => run({ kind: 'review', decision: 'send_back', note }, 'stores.done.sendBack')}
        onClose={close}
      />
    </div>
  );
}

function HoursRow({ day, ranges }: { day: Weekday; ranges: AdminHoursRange[] }) {
  const intl = useIntl();
  const labels = useStoreLabels();
  return (
    <>
      <dt>{labels.day(day)}</dt>
      <dd className={css.mono}>
        {ranges.length === 0
          ? intl.formatMessage({ id: 'stores.hours.closed' })
          : ranges.map((r) => intl.formatMessage({ id: 'stores.hours.range' }, { open: r.open, close: r.close })).join(', ')}
      </dd>
    </>
  );
}

type Week = Record<Weekday, AdminHoursRange[]>;

const LAT = { min: -90, max: 90 };
const LNG = { min: -180, max: 180 };

/** Edit the pin and the weekly hours; holidays go back as they are. The API checks overlaps. */
function EditDialog({
  s,
  change,
  error,
  close,
  run,
}: {
  s: AdminStore;
  change: ReturnType<typeof useStoreChange>;
  error: string | null;
  close: () => void;
  run: (c: StoreChange, done: string) => void;
}) {
  const intl = useIntl();
  const labels = useStoreLabels();
  const [lat, setLat] = useState(s.location ? String(s.location.lat) : '');
  const [lng, setLng] = useState(s.location ? String(s.location.lng) : '');
  const [week, setWeek] = useState<Week>(() => Object.fromEntries(WEEKDAYS.map((d) => [d, [...(s.hours.weekly[d] ?? [])]])) as Week);

  const latN = Number(lat);
  const lngN = Number(lng);
  const noPin = lat.trim() === '' && lng.trim() === '';
  const pinBad =
    !noPin &&
    (lat.trim() === '' || lng.trim() === '' || !Number.isFinite(latN) || !Number.isFinite(lngN) || latN < LAT.min || latN > LAT.max || lngN < LNG.min || lngN > LNG.max);
  const emptyTime = WEEKDAYS.some((d) => week[d].some((r) => !r.open || !r.close));
  // Server field errors name the day: body.hours.weekly.mon[1].
  const fields = change.error instanceof ApiError ? change.error.fields : {};
  const dayBad = (d: Weekday) => Object.keys(fields).some((k) => k.startsWith(`body.hours.weekly.${d}`));
  const pinServerBad = Object.keys(fields).some((k) => k.startsWith('body.location'));

  const set = (d: Weekday, f: (rs: AdminHoursRange[]) => AdminHoursRange[]) => setWeek((w) => ({ ...w, [d]: f(w[d]) }));
  const weekly = (): AdminStoreWeek => Object.fromEntries(WEEKDAYS.map((d) => [d, week[d]])) as AdminStoreWeek;

  return (
    <ReasonDialog
      open
      title={intl.formatMessage({ id: 'stores.edit.title' }, { name: s.name })}
      description={intl.formatMessage({ id: 'stores.edit.body' })}
      confirmLabel={intl.formatMessage({ id: 'stores.edit.save' })}
      blocked={pinBad || emptyTime}
      pending={change.isPending}
      error={error}
      onConfirm={(reason) =>
        run(
          {
            kind: 'edit',
            reason,
            hours: { weekly: weekly(), holidays: s.hours.holidays },
            ...(noPin ? {} : { location: { lat: latN, lng: lngN } }),
          },
          'stores.done.edit',
        )
      }
      onClose={close}
    >
      <fieldset className="tz-field">
        <legend className="tz-field-label">{intl.formatMessage({ id: 'stores.pin' })}</legend>
        <div className={css.form}>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="pin-lat">
              {intl.formatMessage({ id: 'stores.pin.lat' })}
            </label>
            <input id="pin-lat" className="tz-input" inputMode="decimal" value={lat} aria-invalid={pinBad || pinServerBad} onChange={(e) => setLat(e.target.value)} />
          </div>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="pin-lng">
              {intl.formatMessage({ id: 'stores.pin.lng' })}
            </label>
            <input id="pin-lng" className="tz-input" inputMode="decimal" value={lng} aria-invalid={pinBad || pinServerBad} onChange={(e) => setLng(e.target.value)} />
          </div>
        </div>
        <p className={pinBad || pinServerBad ? 'tz-field-error' : 'tz-field-help'}>
          {intl.formatMessage({ id: pinBad || pinServerBad ? 'stores.pin.bad' : 'stores.pin.help' })}
        </p>
      </fieldset>
      <fieldset className="tz-field">
        <legend className="tz-field-label">{intl.formatMessage({ id: 'stores.hours' })}</legend>
        <p className="tz-field-help">{intl.formatMessage({ id: 'stores.hours.help' }, { max: MAX_RANGES })}</p>
        <div className={css.stack}>
          {WEEKDAYS.map((d) => {
            const day = labels.day(d);
            return (
              <div key={d} role="group" aria-label={day} className={css.column}>
                <div className={css.title}>
                  <strong>{day}</strong>
                  {week[d].length === 0 && <span className={css.muted}>{intl.formatMessage({ id: 'stores.hours.closed' })}</span>}
                  <Button
                    size="sm"
                    variant="ghost"
                    icon={Plus}
                    disabled={week[d].length >= MAX_RANGES}
                    aria-label={intl.formatMessage({ id: 'stores.hours.addOne' }, { day })}
                    onClick={() => set(d, (rs) => [...rs, { open: '', close: '' }])}
                  >
                    {intl.formatMessage({ id: 'stores.hours.add' })}
                  </Button>
                </div>
                {week[d].map((r, i) => (
                  <div key={i} className={css.form}>
                    <input
                      className="tz-input"
                      type="time"
                      value={r.open}
                      aria-label={intl.formatMessage({ id: 'stores.hours.opens' }, { day, n: i + 1 })}
                      onChange={(e) => set(d, (rs) => rs.map((x, j) => (j === i ? { ...x, open: e.target.value } : x)))}
                    />
                    <input
                      className="tz-input"
                      type="time"
                      value={r.close}
                      aria-label={intl.formatMessage({ id: 'stores.hours.closes' }, { day, n: i + 1 })}
                      onChange={(e) => set(d, (rs) => rs.map((x, j) => (j === i ? { ...x, close: e.target.value } : x)))}
                    />
                    <IconButton
                      size="sm"
                      icon={Trash2}
                      label={intl.formatMessage({ id: 'stores.hours.remove' }, { day, n: i + 1 })}
                      onClick={() => set(d, (rs) => rs.filter((_, j) => j !== i))}
                    />
                  </div>
                ))}
                {dayBad(d) && (
                  <p className="tz-field-error">{intl.formatMessage({ id: 'stores.hours.bad' }, { day })}</p>
                )}
              </div>
            );
          })}
        </div>
        {emptyTime && <p className="tz-field-help">{intl.formatMessage({ id: 'stores.hours.fill' })}</p>}
      </fieldset>
    </ReasonDialog>
  );
}
