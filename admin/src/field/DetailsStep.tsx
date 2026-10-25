import { ApiError } from '@todayz/api-client';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldBusinessUpdate } from '@todayz/api-client/gen/model/fieldBusinessUpdate';
import { Button, TextField } from '@todayz/ui';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { isOpen, toE164 } from './labels';
import { useCategories, useUpdateBusiness } from './queries';
import css from './field.module.css';

/** Step 1: name, legal name, category; the owner's number can change until the owner confirms. */
export function DetailsStep({ business: b, onDone }: { business: FieldBusiness; onDone: () => void }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const cats = useCategories();
  const save = useUpdateBusiness(b.id);
  const [brand, setBrand] = useState(b.brandName);
  const [legal, setLegal] = useState(b.legalName ?? '');
  const [category, setCategory] = useState(b.categoryId ?? '');
  const [phone, setPhone] = useState('');
  const [shown, setShown] = useState(false);
  const categoryName = cats.data?.items.find((c) => c.id === b.categoryId)?.name;

  if (!isOpen(b.status))
    return (
      <dl className={css.facts}>
        <Fact label={intl.formatMessage({ id: 'field.new.brand' })} value={b.brandName} />
        <Fact label={intl.formatMessage({ id: 'field.new.legal' })} value={b.legalName ?? '—'} />
        <Fact label={intl.formatMessage({ id: 'field.new.category' })} value={categoryName ?? '—'} />
        <Fact label={intl.formatMessage({ id: 'field.owner.number' })} value={b.ownerPhoneMasked ?? '—'} />
      </dl>
    );

  const owner = phone.trim() ? toE164(phone) : undefined;
  const errors = {
    brand: brand.trim() ? undefined : intl.formatMessage({ id: 'field.new.brand.required' }),
    category: category ? undefined : intl.formatMessage({ id: 'field.details.category.required' }),
    phone: owner === null ? intl.formatMessage({ id: 'field.new.phone.invalid' }) : undefined,
  };

  // The API refuses a number that cannot own a business (422 on body.ownerPhone).
  const refused = save.error instanceof ApiError && !!save.error.fields['body.ownerPhone'];

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setShown(true);
    if (errors.brand || errors.category || errors.phone) return;
    const body: FieldBusinessUpdate = {};
    if (brand.trim() !== b.brandName) body.brandName = brand.trim();
    if ((legal.trim() || null) !== b.legalName) body.legalName = legal.trim() || null;
    if (category !== (b.categoryId ?? '')) body.categoryId = category;
    if (owner) body.ownerPhone = owner;
    if (Object.keys(body).length === 0) {
      onDone();
      return;
    }
    save.mutate(body, {
      onSuccess: () => {
        setPhone('');
        onDone();
      },
    });
  };

  return (
    <form className={css.form} onSubmit={submit} noValidate>
      <TextField
        label={intl.formatMessage({ id: 'field.new.brand' })}
        value={brand}
        maxLength={80}
        autoComplete="off"
        onChange={(e) => setBrand(e.target.value)}
        error={shown ? errors.brand : undefined}
      />
      <TextField
        label={intl.formatMessage({ id: 'field.new.legal' })}
        value={legal}
        maxLength={120}
        autoComplete="off"
        onChange={(e) => setLegal(e.target.value)}
      />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="field-details-category">
          {intl.formatMessage({ id: 'field.new.category' })}
        </label>
        <select
          id="field-details-category"
          className={`tz-input${shown && errors.category ? ' tz-input-invalid' : ''}`}
          value={category}
          aria-invalid={(shown && !!errors.category) || undefined}
          onChange={(e) => setCategory(e.target.value)}
        >
          <option value="">{intl.formatMessage({ id: 'field.details.category.pick' })}</option>
          {(cats.data?.items ?? []).map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
        {shown && errors.category && <p className="tz-field-error">{errors.category}</p>}
      </div>
      {!b.ownerConfirmed && (
        <TextField
          label={intl.formatMessage({ id: 'field.details.phone' })}
          help={intl.formatMessage({ id: 'field.details.phone.help' }, { masked: b.ownerPhoneMasked ?? '—' })}
          prefix="+91"
          type="tel"
          inputMode="numeric"
          autoComplete="off"
          maxLength={14}
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          error={(shown && errors.phone) || (refused ? intl.formatMessage({ id: 'field.owner.refused' }) : undefined)}
        />
      )}
      {save.isError && !refused && (
        <p role="alert" className="tz-field-error">
          {save.error instanceof ApiError && Object.keys(save.error.fields).length > 0
            ? intl.formatMessage({ id: 'common.error.invalid' })
            : problemText(save.error)}
        </p>
      )}
      <Button type="submit" variant="primary" block loading={save.isPending}>
        {intl.formatMessage({ id: 'field.saveNext' })}
      </Button>
    </form>
  );
}

export function Fact({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </>
  );
}
