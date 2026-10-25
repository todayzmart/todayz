import type { AdminIntegration } from '@todayz/api-client/gen/model/adminIntegration';
import type { AdminNotificationApp } from '@todayz/api-client/gen/model/adminNotificationApp';
import type { AdminNotificationLang } from '@todayz/api-client/gen/model/adminNotificationLang';
import type { AdminNotificationTemplate } from '@todayz/api-client/gen/model/adminNotificationTemplate';
import type { AdminNotificationText } from '@todayz/api-client/gen/model/adminNotificationText';
import { Button, DataTable, Segmented, StatusPill, Tabs, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Send } from 'lucide-react';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { AuditPanel } from '../audit/AuditPanel';
import { previewParts, useNotifyLabels } from './labels';
import { useIntegrations, useTemplates, useTestSend } from './queries';
import { LANGS, type NotificationsSearch } from './search';
import css from '../app/page.module.css';
import styles from './notifications.module.css';

type Go = (next: NotificationsSearch) => void;
type AppFilter = AdminNotificationApp | 'all';

/**
 * A-16 Notifications, read-only for v1 (D-224): every kind's text in each language, each channel's
 * switch, keys (set or missing, never a value) and last failure. The texts stay in code until DLT
 * re-registration has a process, so nothing here edits them. A test goes to your own in-app inbox only.
 */
export function NotificationsPage({ search }: { search: NotificationsSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const go: Go = useCallback((next) => void navigate({ to: '/notifications', search: next }), [navigate]);
  return (
    <div className={css.column}>
      <div className={css.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.notifications' })}</h1>
          <p className="tz-page-sub">{intl.formatMessage({ id: 'notifications.sub' })}</p>
        </div>
      </div>
      <Integrations />
      <Segmented<AppFilter>
        label={intl.formatMessage({ id: 'notifications.app' })}
        options={[
          { value: 'all', label: intl.formatMessage({ id: 'notifications.app.all' }) },
          { value: 'customer', label: intl.formatMessage({ id: 'notifications.app.customer' }) },
          { value: 'business', label: intl.formatMessage({ id: 'notifications.app.business' }) },
        ]}
        value={search.app ?? 'all'}
        onChange={(v) => go(v === 'all' ? {} : { app: v })}
      />
      <Templates search={search} go={go} />
    </div>
  );
}

/** One row per outside channel; the in-app inbox is always written and has no switch. */
function Integrations() {
  const intl = useIntl();
  const when = useWhen();
  const labels = useNotifyLabels();
  const problemText = useProblemText();
  const q = useIntegrations();
  return (
    <section className="tz-card" aria-labelledby="integrations-title">
      <h2 id="integrations-title" className="tz-card-title">
        {intl.formatMessage({ id: 'notifications.integrations' })}
      </h2>
      <p className={css.muted}>{intl.formatMessage({ id: 'notifications.integrations.sub' })}</p>
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <ul className={styles.channels} aria-busy={q.isPending}>
          {(q.data?.items ?? []).map((c) => (
            <Channel key={c.channel} c={c} name={labels.channel(c.channel)} now={q.data?.serverNow ?? ''} when={when} />
          ))}
        </ul>
      )}
    </section>
  );
}

function Channel({ c, name, now, when }: { c: AdminIntegration; name: string; now: string; when: (at: string, now: string) => string }) {
  const intl = useIntl();
  return (
    <li className={styles.channel} aria-labelledby={`channel-${c.channel}`}>
      <div className={css.title}>
        <h3 id={`channel-${c.channel}`} className={styles.channelName}>
          {name}
        </h3>
        <StatusPill tone={c.enabled ? 'completed' : 'neutral'}>
          {intl.formatMessage({ id: c.enabled ? 'notifications.switch.on' : 'notifications.switch.off' })}
        </StatusPill>
        {!c.senderBuilt && <StatusPill tone="pending">{intl.formatMessage({ id: 'notifications.notBuilt' })}</StatusPill>}
      </div>
      <ul className={styles.vars}>
        {c.vars.map((v) => (
          <li key={v.name} className={styles.var}>
            <span className={styles.code}>{v.name}</span>
            <StatusPill tone={v.set ? 'completed' : 'pending'}>
              {intl.formatMessage({ id: v.set ? 'notifications.var.set' : 'notifications.var.missing' })}
            </StatusPill>
          </li>
        ))}
      </ul>
      <p className={css.muted}>
        {c.lastError
          ? intl.formatMessage({ id: 'notifications.lastError' }, { error: c.lastError.error, when: when(c.lastError.at, now || c.lastError.at) })
          : intl.formatMessage({ id: 'notifications.noError' })}
      </p>
    </li>
  );
}

function Templates({ search, go }: { search: NotificationsSearch; go: Go }) {
  const intl = useIntl();
  const labels = useNotifyLabels();
  const problemText = useProblemText();
  const q = useTemplates(search.app);
  const rows = useMemo(() => q.data?.items ?? [], [q.data]);
  const selected = search.kind ? rows.find((t) => t.kind === search.kind) : undefined;

  const columns = useMemo<ColumnDef<AdminNotificationTemplate, unknown>[]>(
    () => [
      {
        id: 'kind',
        header: intl.formatMessage({ id: 'notifications.col.kind' }),
        cell: ({ row }) => (
          <div>
            <Button size="sm" variant="ghost" aria-pressed={row.original.kind === search.kind} onClick={() => go({ ...search, kind: row.original.kind })}>
              {labels.kind(row.original.kind)}
            </Button>
            <div className={`${css.muted} ${styles.code}`}>{row.original.kind}</div>
          </div>
        ),
      },
      {
        id: 'app',
        header: intl.formatMessage({ id: 'notifications.col.app' }),
        accessorFn: (t) => intl.formatMessage({ id: t.app === 'customer' ? 'notifications.app.customer' : 'notifications.app.business' }),
      },
      {
        id: 'channels',
        header: intl.formatMessage({ id: 'notifications.col.channels' }),
        accessorFn: (t) => [intl.formatMessage({ id: 'notifications.channel.inApp' }), ...t.channels.map(labels.channel)].join(', '),
      },
      {
        id: 'langs',
        header: intl.formatMessage({ id: 'notifications.col.langs' }),
        accessorFn: (t) => intl.formatMessage({ id: 'notifications.langsOwn' }, { own: ownLangs(t).length, all: LANGS.length }),
      },
    ],
    [intl, labels, search, go],
  );

  return (
    <div className={styles.detail}>
      <div className={css.column}>
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'notifications.kinds' })}
            columns={columns}
            data={rows}
            getRowId={(t) => t.kind}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'notifications.empty' })}
          />
        )}
      </div>
      <div className={css.column}>
        {selected ? (
          <>
            <TemplateCard key={selected.kind} t={selected} lang={search.lang ?? 'en'} onLang={(l) => go(withLang(search, l))} />
            <AuditPanel type="notification_template" id={selected.kind} />
          </>
        ) : (
          <p className={`tz-card ${css.muted}`}>
            {intl.formatMessage({ id: search.kind && !q.isPending ? 'notifications.pick.notListed' : 'notifications.pick' })}
          </p>
        )}
      </div>
    </div>
  );
}

function withLang({ lang: _, ...rest }: NotificationsSearch, l: AdminNotificationLang): NotificationsSearch {
  return l === 'en' ? rest : { ...rest, lang: l };
}

/** Languages with a text of their own for the kind's base text (the rest show English). */
function ownLangs(t: AdminNotificationTemplate): AdminNotificationLang[] {
  return t.texts.filter((x) => x.variant === null && !x.fallback).map((x) => x.lang);
}

function TemplateCard({ t, lang, onLang }: { t: AdminNotificationTemplate; lang: AdminNotificationLang; onLang: (l: AdminNotificationLang) => void }) {
  const intl = useIntl();
  const labels = useNotifyLabels();
  const own = ownLangs(t);
  return (
    <section className="tz-card" aria-labelledby={`kind-${t.kind}`}>
      <h2 id={`kind-${t.kind}`} className="tz-card-title">
        {labels.kind(t.kind)}
      </h2>
      <dl className={css.facts}>
        <dt>{intl.formatMessage({ id: 'notifications.col.app' })}</dt>
        <dd>{intl.formatMessage({ id: t.app === 'customer' ? 'notifications.app.customer' : 'notifications.app.business' })}</dd>
        <dt>{intl.formatMessage({ id: 'notifications.col.channels' })}</dt>
        <dd>{[intl.formatMessage({ id: 'notifications.channel.inApp' }), ...t.channels.map(labels.channel)].join(', ')}</dd>
        <dt>{intl.formatMessage({ id: 'notifications.params' })}</dt>
        <dd className={styles.params}>
          {t.params.length === 0
            ? intl.formatMessage({ id: 'notifications.params.none' })
            : t.params.map((p) => (
                <span key={p} className={styles.param}>
                  {p}
                </span>
              ))}
        </dd>
        {t.providerTemplates.length > 0 && (
          <>
            <dt>{intl.formatMessage({ id: 'notifications.providerTemplates' })}</dt>
            <dd>
              <ul className={styles.vars}>
                {t.providerTemplates.map((p) => (
                  <li key={p.lang} className={styles.var}>
                    <span>
                      {labels.lang(p.lang)} · <span className={styles.code}>{p.name}</span>
                    </span>
                    <StatusPill tone={p.smsTemplateIdSet ? 'completed' : 'pending'}>
                      {intl.formatMessage({ id: p.smsTemplateIdSet ? 'notifications.dlt.set' : 'notifications.dlt.missing' })}
                    </StatusPill>
                  </li>
                ))}
              </ul>
            </dd>
          </>
        )}
      </dl>
      <Tabs
        label={intl.formatMessage({ id: 'notifications.langs' })}
        tabs={LANGS.map((l) => ({
          value: l,
          label: labels.lang(l),
          panel: <Texts t={t} texts={t.texts.filter((x) => x.lang === l)} note={own.includes(l) ? null : intl.formatMessage({ id: 'notifications.lang.noOwn' }, { lang: labels.lang(l) })} />,
        }))}
        value={lang}
        onChange={onLang}
      />
    </section>
  );
}

/** The kind's own text, then each variant's, unrendered: `{param}` shows as a chip. */
function Texts({ t, texts, note }: { t: AdminNotificationTemplate; texts: AdminNotificationText[]; note: string | null }) {
  const intl = useIntl();
  const toast = useToast();
  const problemText = useProblemText();
  const send = useTestSend();
  const test = (variant: string | null) =>
    send.mutate(
      { kind: t.kind, variant },
      { onSuccess: () => toast.show({ message: intl.formatMessage({ id: 'notifications.test.done' }) }) },
    );
  return (
    <div className={css.stack}>
      {note && <p className={css.muted}>{note}</p>}
      {t.testable ? (
        <p className={css.muted}>{intl.formatMessage({ id: 'notifications.test.help' })}</p>
      ) : (
        <p className={css.muted}>{intl.formatMessage({ id: 'notifications.test.business' })}</p>
      )}
      {send.isError && (
        <p role="alert" className="tz-field-error">
          {problemText(send.error)}
        </p>
      )}
      {texts.map((x) => (
        <article key={x.variant ?? ''} className={styles.text} aria-label={x.variant ?? intl.formatMessage({ id: 'notifications.variant.base' })}>
          <div className={css.title}>
            <span className={styles.variant}>{x.variant ?? intl.formatMessage({ id: 'notifications.variant.base' })}</span>
            {x.fallback && <StatusPill tone="pending">{intl.formatMessage({ id: 'notifications.fallback' })}</StatusPill>}
          </div>
          <p className={styles.previewTitle}>
            <Preview s={x.title} />
          </p>
          <p className={styles.previewBody}>
            <Preview s={x.body} />
          </p>
          {t.testable && (
            <div className={css.actions}>
              <Button size="sm" variant="soft" icon={Send} loading={send.isPending && send.variables?.variant === x.variant} disabled={send.isPending} onClick={() => test(x.variant)}>
                {intl.formatMessage({ id: 'notifications.test.send' })}
              </Button>
            </div>
          )}
        </article>
      ))}
    </div>
  );
}

function Preview({ s }: { s: string }) {
  return (
    <>
      {previewParts(s).map((p, i) =>
        p.param ? (
          <span key={i} className={styles.param}>
            {p.text}
          </span>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  );
}
