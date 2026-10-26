import { confirmCatalogueCleanupDraft, createCatalogueCleanupDraft, getCatalogueCleanupDraft } from '@todayz/api-client/gen/ai/ai';
import type { AdminAiCategoryNames } from '@todayz/api-client/gen/model/adminAiCategoryNames';
import type { AdminAiUnit } from '@todayz/api-client/gen/model/adminAiUnit';
import type { AdminPendingProduct } from '@todayz/api-client/gen/model/adminPendingProduct';
import type { CatalogueCleanupDraft } from '@todayz/api-client/gen/model/catalogueCleanupDraft';
import type { CatalogueCleanupLineValue } from '@todayz/api-client/gen/model/catalogueCleanupLineValue';
import { Button, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Check } from 'lucide-react';
import { Fragment, useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { LANGS, UNITS, useCatalogueLabels } from '../../catalogue/labels';
import { pendingKey } from '../../catalogue/queries';
import { AssistCard, AssistLine } from '../AssistCard';
import { assistErrorKey, decision, lineReasons, useAssistDraft, type Choice } from '../draft';
import css from '../assist.module.css';

/** The contract's bounds on a drafted edit (CatalogueCleanupLineValue, AdminAiCategoryNames). */
const MAX_NAME = 120;
const MAX_VARIANT = 80;
const MAX_LANG = 60;

const trimNames = (m: AdminAiCategoryNames): AdminAiCategoryNames => ({ en: m.en.trim(), hi: m.hi.trim(), kn: m.kn.trim(), te: m.te.trim(), ta: m.ta.trim() });

/**
 * AI-A1 in the A-08 review detail (D-233): the pending product's fields normalised, the active
 * product it duplicates with a reason, and its category's missing names. Each line is the
 * reviewer's to use, edit or skip; using normalise and merge together is refused (merge or edit).
 */
export function CatalogueAssist({ pending }: { pending: AdminPendingProduct }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const id = pending.product.id;
  const a = useAssistDraft<CatalogueCleanupDraft, Parameters<typeof confirmCatalogueCleanupDraft>[1]>({
    key: ['admin', 'ai', 'catalogue', id],
    create: (headers) => createCatalogueCleanupDraft({ pendingId: id }, { headers }),
    get: (draftId) => getCatalogueCleanupDraft(draftId),
    confirm: (draftId, body, headers) => confirmCatalogueCleanupDraft(draftId, body, { headers }),
    settled: () => {
      void qc.invalidateQueries({ queryKey: pendingKey });
      void qc.invalidateQueries({ queryKey: ['admin', 'products'] });
      void qc.invalidateQueries({ queryKey: ['admin', 'categories'] });
    },
  });
  return (
    <AssistCard
      title={intl.formatMessage({ id: 'aiAssist.catalogue.title' })}
      intro={intl.formatMessage({ id: 'aiAssist.catalogue.intro' })}
      startLabel={intl.formatMessage({ id: 'aiAssist.catalogue.start' })}
      start={a.start}
      reset={a.reset}
      working={a.working}
      error={a.error}
      status={a.draft?.status}
      result={a.confirm.data}
    >
      {a.draft && <Lines key={a.draft.draftId} d={a.draft} pending={pending} confirm={a.confirm} />}
    </AssistCard>
  );
}

type Confirm = ReturnType<typeof useAssistDraft<CatalogueCleanupDraft, Parameters<typeof confirmCatalogueCleanupDraft>[1]>>['confirm'];

function Lines({ d, pending, confirm }: { d: CatalogueCleanupDraft; pending: AdminPendingProduct; confirm: Confirm }) {
  const intl = useIntl();
  const fid = useId();
  const toast = useToast();
  const labels = useCatalogueLabels();
  const t = (key: string, values?: Record<string, string | number>) => intl.formatMessage({ id: key }, values);
  const n = d.normalise;
  const dup = d.duplicate;
  const names = d.categoryNames;
  // A line the server already settled (applied, or skipped on an earlier confirm) is not offered again.
  const done = new Set(d.outcomes.filter((o) => o.outcome !== 'not_permitted').map((o) => o.lineId));
  const [choice, setChoice] = useState<Record<string, Choice>>({});
  const pick = (lineId: string) => choice[lineId] ?? 'discarded';
  const choose = (lineId: string) => (c: Choice) => setChoice((m) => ({ ...m, [lineId]: c }));

  const [name, setName] = useState(n?.name ?? '');
  const [variant, setVariant] = useState(n?.variant ?? '');
  const [pack, setPack] = useState(n ? String(n.packSize) : '');
  const [unit, setUnit] = useState<AdminAiUnit>(n?.unit ?? 'pc');
  const [into, setInto] = useState(dup?.productId ?? '');
  const [langNames, setLangNames] = useState<AdminAiCategoryNames>(names?.names ?? { en: '', hi: '', kn: '', te: '', ta: '' });

  const live = [n, dup, names].filter((l): l is NonNullable<typeof l> => l !== null && !done.has(l.lineId));
  const using = (l: { lineId: string } | null) => l !== null && !done.has(l.lineId) && pick(l.lineId) !== 'discarded';
  const both = using(n) && using(dup);
  const packNum = Number(pack);
  const normaliseBad = n !== null && pick(n.lineId) === 'edited' && (name.trim() === '' || name.trim().length > MAX_NAME || variant.trim().length > MAX_VARIANT || !(packNum > 0));
  const namesBad = names !== null && pick(names.lineId) === 'edited' && LANGS.some((l) => langNames[l].trim() === '' || langNames[l].trim().length > MAX_LANG);
  const reasons = lineReasons(confirm.error);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (both || normaliseBad || namesBad || confirm.isPending) return;
    const lines = live.map((l) => {
      const c = 'canApply' in l && !l.canApply ? 'discarded' : pick(l.lineId);
      let v: CatalogueCleanupLineValue = {};
      if (l === n) v = { name: name.trim(), variant: variant.trim() || null, packSize: packNum, unit, categoryId: n.categoryId };
      if (l === dup) v = { productId: into };
      if (l === names) v = { names: trimNames(langNames) };
      return decision(l.lineId, c, v);
    });
    confirm.mutate(
      { lines },
      {
        onSuccess: (r) => {
          if (r.status !== 'ready') toast.show({ message: t('aiAssist.toast.done') });
        },
      },
    );
  };
  const reasonFor = (lineId: string) => {
    const i = live.findIndex((l) => l.lineId === lineId);
    const r = reasons[i] ?? confirm.data?.lines.find((x) => x.lineId === lineId && x.outcome === 'not_permitted')?.reason;
    return r ? t(r === 'not_permitted' ? 'aiAssist.notPermitted' : 'aiAssist.lineInvalid') : undefined;
  };

  if (live.length === 0) return <p className={css.muted}>{t('aiAssist.nothing')}</p>;
  return (
    <form className={css.stack} onSubmit={submit} noValidate>
      {n && !done.has(n.lineId) && (
        <AssistLine
          title={t('aiAssist.catalogue.normalise')}
          choice={pick(n.lineId)}
          onChoice={choose(n.lineId)}
          canApply={n.canApply}
          useLabel={t('aiAssist.catalogue.useFields')}
          problem={reasonFor(n.lineId)}
          edit={
            <div className={css.fields}>
              <TextField label={t('aiAssist.catalogue.name')} value={name} maxLength={MAX_NAME} onChange={(e) => setName(e.target.value)} />
              <TextField label={t('aiAssist.catalogue.variant')} help={t('aiAssist.optional')} value={variant} maxLength={MAX_VARIANT} onChange={(e) => setVariant(e.target.value)} />
              <TextField label={t('aiAssist.catalogue.pack')} inputMode="decimal" value={pack} onChange={(e) => setPack(e.target.value)} />
              <div className="tz-field">
                <label className="tz-field-label" htmlFor={`${fid}-unit`}>
                  {t('aiAssist.catalogue.unit')}
                </label>
                <select id={`${fid}-unit`} className="tz-input" value={unit} onChange={(e) => setUnit(e.target.value as AdminAiUnit)}>
                  {UNITS.map((u) => (
                    <option key={u} value={u}>
                      {u}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          }
        >
          <dl className={css.facts}>
            <dt>{t('aiAssist.catalogue.name')}</dt>
            <dd>{labels.name(n)}</dd>
            <dt>{t('aiAssist.catalogue.brand')}</dt>
            <dd>{n.brandName ?? t('aiAssist.none')}</dd>
            <dt>{t('aiAssist.catalogue.pack')}</dt>
            <dd>{labels.pack(n)}</dd>
            <dt>{t('aiAssist.catalogue.category')}</dt>
            <dd>{n.categoryName}</dd>
          </dl>
        </AssistLine>
      )}
      {dup && !done.has(dup.lineId) && (
        <AssistLine
          title={t('aiAssist.catalogue.duplicate')}
          choice={pick(dup.lineId)}
          onChoice={choose(dup.lineId)}
          canApply={dup.canApply}
          useLabel={t('aiAssist.catalogue.useMerge')}
          problem={reasonFor(dup.lineId)}
          edit={
            <div className="tz-field">
              <label className="tz-field-label" htmlFor={`${fid}-into`}>
                {t('aiAssist.catalogue.into')}
              </label>
              <select id={`${fid}-into`} className="tz-input" value={into} onChange={(e) => setInto(e.target.value)}>
                {pending.suggestions.map((s) => (
                  <option key={s.product.id} value={s.product.id}>
                    {labels.name(s.product)} · {labels.pack(s.product)}
                  </option>
                ))}
              </select>
            </div>
          }
        >
          <p>{t('aiAssist.catalogue.mergeInto', { name: dup.productName, similarity: labels.similarity(dup.similarity) })}</p>
          <p className={css.muted}>{dup.reason}</p>
        </AssistLine>
      )}
      {names && !done.has(names.lineId) && (
        <AssistLine
          title={t('aiAssist.catalogue.names')}
          choice={pick(names.lineId)}
          onChoice={choose(names.lineId)}
          canApply={names.canApply}
          useLabel={t('aiAssist.catalogue.useNames')}
          problem={reasonFor(names.lineId)}
          edit={
            <div className={css.fields}>
              {LANGS.map((g) => (
                <TextField key={g} label={labels.lang(g)} value={langNames[g]} maxLength={MAX_LANG} onChange={(e) => setLangNames((m) => ({ ...m, [g]: e.target.value }))} />
              ))}
            </div>
          }
        >
          <dl className={css.facts}>
            {LANGS.map((g) => (
              <Fragment key={g}>
                <dt>{labels.lang(g)}</dt>
                <dd lang={g}>{names.names[g]}</dd>
              </Fragment>
            ))}
          </dl>
        </AssistLine>
      )}
      {both && (
        <p role="alert" className="tz-field-error">
          {t('aiAssist.catalogue.mergeOrEdit')}
        </p>
      )}
      {(normaliseBad || namesBad) && (
        <p role="alert" className="tz-field-error">
          {t('aiAssist.lineInvalid')}
        </p>
      )}
      {confirm.error !== null && Object.keys(reasons).length === 0 && (
        <p role="alert" className="tz-field-error">
          {t(`aiAssist.error.${assistErrorKey(confirm.error)}`)}
        </p>
      )}
      <div className={css.actions}>
        <Button type="submit" variant="primary" icon={Check} loading={confirm.isPending} disabled={both}>
          {t('aiAssist.confirm')}
        </Button>
      </div>
    </form>
  );
}
