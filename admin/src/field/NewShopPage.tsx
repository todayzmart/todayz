import { ApiError } from '@todayz/api-client';
import { Button, TextField } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { toE164 } from './labels';
import { useCategories, useCreateBusiness } from './queries';
import css from './field.module.css';

/** A-04 step 1: the shop's name, category and the owner's number (the code goes there at the end). */
export function NewShopPage() {
  const intl = useIntl();
  const navigate = useNavigate();
  const problemText = useProblemText();
  const cats = useCategories();
  const create = useCreateBusiness();
  const [brand, setBrand] = useState('');
  const [legal, setLegal] = useState('');
  const [category, setCategory] = useState('');
  const [phone, setPhone] = useState('');
  const [shown, setShown] = useState(false);

  const owner = toE164(phone);
  const errors = {
    brand: brand.trim() ? undefined : intl.formatMessage({ id: 'field.new.brand.required' }),
    phone: owner ? undefined : intl.formatMessage({ id: 'field.new.phone.invalid' }),
  };
  const server = create.error instanceof ApiError ? create.error.fields : {};

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setShown(true);
    if (errors.brand || errors.phone || !owner) return;
    create.mutate(
      {
        brandName: brand.trim(),
        ownerPhone: owner,
        ...(legal.trim() ? { legalName: legal.trim() } : {}),
        ...(category ? { categoryId: category } : {}),
      },
      { onSuccess: (b) => void navigate({ to: '/field/$businessId', params: { businessId: b.id }, replace: true }) },
    );
  };

  return (
    <div className={css.page}>
      <Link to="/field" className={css.muted}>
        <ArrowLeft size={14} aria-hidden="true" /> {intl.formatMessage({ id: 'field.back' })}
      </Link>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'field.new.title' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'field.new.sub' })}</p>
      </div>
      <form className={`tz-card ${css.form}`} onSubmit={submit} noValidate>
        <TextField
          label={intl.formatMessage({ id: 'field.new.brand' })}
          value={brand}
          maxLength={80}
          autoComplete="off"
          onChange={(e) => setBrand(e.target.value)}
          error={(shown && errors.brand) || (server['body.brandName'] && intl.formatMessage({ id: 'common.error.invalid' }))}
        />
        <TextField
          label={intl.formatMessage({ id: 'field.new.legal' })}
          help={intl.formatMessage({ id: 'field.new.legal.help' })}
          value={legal}
          maxLength={120}
          autoComplete="off"
          onChange={(e) => setLegal(e.target.value)}
        />
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="field-category">
            {intl.formatMessage({ id: 'field.new.category' })}
          </label>
          <select id="field-category" className="tz-input" value={category} onChange={(e) => setCategory(e.target.value)}>
            <option value="">{intl.formatMessage({ id: 'field.new.category.later' })}</option>
            {(cats.data?.items ?? []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
        <TextField
          label={intl.formatMessage({ id: 'field.new.phone' })}
          help={intl.formatMessage({ id: 'field.new.phone.help' })}
          prefix="+91"
          type="tel"
          inputMode="numeric"
          autoComplete="off"
          maxLength={14}
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          error={(shown && errors.phone) || (server['body.ownerPhone'] && intl.formatMessage({ id: 'field.owner.refused' }))}
        />
        {create.isError && !(create.error instanceof ApiError && Object.keys(create.error.fields).length > 0) && (
          <p role="alert" className="tz-field-error">
            {problemText(create.error)}
          </p>
        )}
        <Button type="submit" variant="primary" block loading={create.isPending}>
          {intl.formatMessage({ id: 'field.new.submit' })}
        </Button>
      </form>
    </div>
  );
}
