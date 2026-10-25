import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import { Banner, Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { ArrowLeft, Check, SearchX } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { ApiError } from '@todayz/api-client';
import { useProblemText } from '../app/problems';
import { DetailsStep } from './DetailsStep';
import { ItemsStep } from './ItemsStep';
import { businessTone, useFieldLabels } from './labels';
import { OwnerStep } from './OwnerStep';
import { PhotosStep } from './PhotosStep';
import { useFieldBusiness } from './queries';
import { firstStep, STEP_LABEL, STEPS, stepDone, type Step } from './steps';
import { StoreStep } from './StoreStep';
import css from './field.module.css';

/**
 * A-04 for one shop: details, store, photos, items, then the owner's code. Items, the store and
 * photos stay open to the agent until the edit window closes (D-39); the server decides that.
 */
export function ShopPage({ id }: { id: string }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useFieldBusiness(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) {
    if (q.error instanceof ApiError && (q.error.status === 404 || q.error.status === 403))
      return (
        <EmptyState
          icon={SearchX}
          title={intl.formatMessage({ id: 'field.shop.missing.title' })}
          description={intl.formatMessage({ id: 'field.shop.missing.body' })}
          action={
            <Link to="/field" className="tz-btn">
              {intl.formatMessage({ id: 'field.back' })}
            </Link>
          }
        />
      );
    return (
      <p role="alert" className="tz-field-error">
        {problemText(q.error)}{' '}
        <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'common.retry' })}
        </Button>
      </p>
    );
  }
  return <Wizard business={q.data} />;
}

function Wizard({ business: b }: { business: FieldBusiness }) {
  const intl = useIntl();
  const labels = useFieldLabels();
  const [step, setStep] = useState<Step>(() => firstStep(b));
  const until = b.editableUntil && intl.formatDate(b.editableUntil, { dateStyle: 'medium', timeZone: 'Asia/Kolkata' });
  const next = (s: Step) => {
    const i = STEPS.indexOf(s);
    setStep(STEPS[i + 1] ?? s);
  };
  return (
    <div className={css.page}>
      <Link to="/field" className={css.muted}>
        <ArrowLeft size={14} aria-hidden="true" /> {intl.formatMessage({ id: 'field.back' })}
      </Link>
      <div className={css.head}>
        <h1 className="tz-page-title">{b.brandName}</h1>
        <StatusPill tone={businessTone(b.status)}>{labels.status(b.status)}</StatusPill>
      </div>
      {until && (
        <Banner tone={b.itemsEditable ? 'info' : 'warning'}>
          {intl.formatMessage({ id: b.itemsEditable ? 'field.window.open' : 'field.window.closed' }, { until })}
        </Banner>
      )}
      {b.status === 'needs_info' && <Banner tone="warning">{intl.formatMessage({ id: 'field.needsInfo' })}</Banner>}
      <ol className={css.steps} aria-label={intl.formatMessage({ id: 'field.steps' })}>
        {STEPS.map((s, i) => (
          <li key={s}>
            <button type="button" className={css.stepTab} aria-current={s === step ? 'step' : undefined} onClick={() => setStep(s)}>
              {stepDone(b, s) ? <Check size={14} className={css.stepDone} aria-label={intl.formatMessage({ id: 'field.step.done' })} /> : `${i + 1}.`}{' '}
              {intl.formatMessage({ id: STEP_LABEL[s] })}
            </button>
          </li>
        ))}
      </ol>
      <section className="tz-card" aria-label={intl.formatMessage({ id: STEP_LABEL[step] })}>
        <h2 className="tz-card-title">{intl.formatMessage({ id: STEP_LABEL[step] })}</h2>
        {step === 'details' && <DetailsStep business={b} onDone={() => next('details')} />}
        {step === 'store' && <StoreStep business={b} onDone={() => next('store')} />}
        {step === 'photos' && <PhotosStep business={b} onDone={() => next('photos')} />}
        {step === 'items' && <ItemsStep business={b} onDone={() => next('items')} />}
        {step === 'owner' && <OwnerStep business={b} goTo={setStep} />}
      </section>
    </div>
  );
}
