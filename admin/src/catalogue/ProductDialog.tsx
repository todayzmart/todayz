import type { AdminCategory } from '@todayz/api-client/gen/model/adminCategory';
import type { AdminProduct } from '@todayz/api-client/gen/model/adminProduct';
import type { AdminProductCreate } from '@todayz/api-client/gen/model/adminProductCreate';
import type { AdminProductUnit } from '@todayz/api-client/gen/model/adminProductUnit';
import type { AdminProductUpdate } from '@todayz/api-client/gen/model/adminProductUpdate';
import { Button, Dialog, Segmented, TextField } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { MAX_REASON } from '../app/ReasonDialog';
import { parseBarcodes, parseMrp, parsePack, rupees } from './form';
import { UNITS, useCatalogueLabels } from './labels';

export type ProductSave = { kind: 'create'; body: AdminProductCreate } | { kind: 'update'; body: AdminProductUpdate };

export interface ProductDialogProps {
  /** Absent adds a product. */
  product?: AdminProduct;
  categories: readonly AdminCategory[];
  pending: boolean;
  /** The last attempt's failure, already in words. */
  error: string | null;
  onSave: (save: ProductSave) => void;
  onClose: () => void;
}

/**
 * Add or edit a master product. An edit sends only the fields that changed, with a reason for the
 * audit log; a cleared variant or MRP is sent as null. Status moves only between active and retired.
 */
export function ProductDialog({ product: p, categories, pending, error, onSave, onClose }: ProductDialogProps) {
  const intl = useIntl();
  const id = useId();
  const labels = useCatalogueLabels();
  const [name, setName] = useState(p?.name ?? '');
  const [categoryId, setCategoryId] = useState(p?.categoryId ?? '');
  const [variant, setVariant] = useState(p?.variant ?? '');
  const [pack, setPack] = useState(p ? String(p.packSize) : '');
  const [unit, setUnit] = useState<AdminProductUnit>(p?.unit ?? 'pc');
  const [mrp, setMrp] = useState(rupees(p?.mrpPaise ?? null));
  const [barcodes, setBarcodes] = useState(p?.barcodes.join(', ') ?? '');
  const [retired, setRetired] = useState(p?.status === 'retired');
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);

  const cleanName = name.trim();
  const cleanVariant = variant.trim();
  const packP = parsePack(pack);
  const mrpP = parseMrp(mrp);
  const codesP = parseBarcodes(barcodes);
  const cleanReason = reason.trim();
  const nameBad = cleanName.length === 0 || cleanName.length > 120;
  const categoryBad = categoryId === '';
  const reasonBad = !!p && (cleanReason.length === 0 || cleanReason.length > MAX_REASON);
  /** Only an active or retired product changes status here; the queue decides the rest. */
  const statusEditable = p?.status === 'active' || p?.status === 'retired';

  const build = (): ProductSave | null => {
    if (nameBad || categoryBad || 'error' in packP || 'error' in mrpP || 'error' in codesP || reasonBad) return null;
    if (!p) {
      const body: AdminProductCreate = { name: cleanName, categoryId, packSize: packP.value, unit };
      if (cleanVariant) body.variant = cleanVariant;
      if (mrpP.value !== null) body.mrpPaise = mrpP.value;
      if (codesP.value.length) body.barcodes = codesP.value;
      return { kind: 'create', body };
    }
    const body: AdminProductUpdate = { reason: cleanReason };
    if (cleanName !== p.name) body.name = cleanName;
    if (categoryId !== p.categoryId) body.categoryId = categoryId;
    if ((cleanVariant || null) !== p.variant) body.variant = cleanVariant || null;
    if (packP.value !== p.packSize) body.packSize = packP.value;
    if (unit !== p.unit) body.unit = unit;
    if (mrpP.value !== p.mrpPaise) body.mrpPaise = mrpP.value;
    if (codesP.value.join(',') !== p.barcodes.join(',')) body.barcodes = codesP.value;
    if (statusEditable && retired !== (p.status === 'retired')) body.status = retired ? 'retired' : 'active';
    return Object.keys(body).length > 1 ? { kind: 'update', body } : null;
  };
  const save = build();
  const unchanged = !!p && save === null && !nameBad && !categoryBad && !('error' in packP) && !('error' in mrpP) && !('error' in codesP) && !reasonBad;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (save && !pending) onSave(save);
  };
  const show = (bad: boolean, msg: string) => (touched && bad ? intl.formatMessage({ id: msg }) : undefined);

  return (
    <Dialog
      open
      onClose={onClose}
      title={p ? intl.formatMessage({ id: 'catalogue.product.editTitle' }, { name: labels.name(p) }) : intl.formatMessage({ id: 'catalogue.product.addTitle' })}
      description={intl.formatMessage({ id: p ? 'catalogue.product.editBody' : 'catalogue.product.addBody' })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'common.cancel' })}
          </Button>
          <Button type="submit" form={id} variant="primary" loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: p ? 'catalogue.product.save' : 'catalogue.product.add' })}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate>
        <TextField
          label={intl.formatMessage({ id: 'catalogue.field.name' })}
          value={name}
          maxLength={120}
          onChange={(e) => setName(e.target.value)}
          error={show(nameBad, 'catalogue.field.nameError')}
        />
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-category`}>
            {intl.formatMessage({ id: 'catalogue.field.category' })}
          </label>
          <select
            id={`${id}-category`}
            className="tz-input"
            value={categoryId}
            aria-invalid={touched && categoryBad}
            onChange={(e) => setCategoryId(e.target.value)}
          >
            <option value="">{intl.formatMessage({ id: 'catalogue.field.categoryPick' })}</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {labels.category(c, categories)}
              </option>
            ))}
          </select>
          {touched && categoryBad && <p className="tz-field-error">{intl.formatMessage({ id: 'catalogue.field.categoryError' })}</p>}
        </div>
        <TextField
          label={intl.formatMessage({ id: 'catalogue.field.variant' })}
          help={intl.formatMessage({ id: 'catalogue.field.optional' })}
          value={variant}
          maxLength={80}
          onChange={(e) => setVariant(e.target.value)}
        />
        <TextField
          label={intl.formatMessage({ id: 'catalogue.field.pack' })}
          inputMode="decimal"
          className="tz-num"
          maxLength={12}
          value={pack}
          onChange={(e) => setPack(e.target.value)}
          error={show('error' in packP, 'catalogue.field.packError')}
        />
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-unit`}>
            {intl.formatMessage({ id: 'catalogue.field.unit' })}
          </label>
          <select id={`${id}-unit`} className="tz-input" value={unit} onChange={(e) => setUnit(e.target.value as AdminProductUnit)}>
            {UNITS.map((u) => (
              <option key={u} value={u}>
                {u}
              </option>
            ))}
          </select>
        </div>
        <TextField
          label={intl.formatMessage({ id: 'catalogue.field.mrp' })}
          prefix="₹"
          inputMode="decimal"
          className="tz-num"
          maxLength={14}
          value={mrp}
          onChange={(e) => setMrp(e.target.value)}
          help={intl.formatMessage({ id: 'catalogue.field.optional' })}
          error={show('error' in mrpP, 'catalogue.field.mrpError')}
        />
        <TextField
          label={intl.formatMessage({ id: 'catalogue.field.barcodes' })}
          inputMode="numeric"
          value={barcodes}
          onChange={(e) => setBarcodes(e.target.value)}
          help={intl.formatMessage({ id: 'catalogue.field.barcodesHelp' })}
          error={
            touched && 'error' in codesP
              ? intl.formatMessage({ id: codesP.error === 'barcodes' ? 'catalogue.field.barcodesMany' : 'catalogue.field.barcodeError' })
              : undefined
          }
        />
        {p && statusEditable && (
          <Segmented
            label={intl.formatMessage({ id: 'catalogue.field.status' })}
            options={[
              { value: 'active', label: labels.status('active') },
              { value: 'retired', label: labels.status('retired') },
            ]}
            value={retired ? 'retired' : 'active'}
            onChange={(v) => setRetired(v === 'retired')}
          />
        )}
        {p && (
          <div className="tz-field">
            <label className="tz-field-label" htmlFor={`${id}-reason`}>
              {intl.formatMessage({ id: 'common.reason.label' })}
            </label>
            <textarea
              id={`${id}-reason`}
              className="tz-input"
              rows={3}
              maxLength={MAX_REASON}
              value={reason}
              aria-invalid={touched && reasonBad}
              aria-describedby={`${id}-reason-help`}
              onChange={(e) => setReason(e.target.value)}
            />
            <p id={`${id}-reason-help`} className={touched && reasonBad ? 'tz-field-error' : 'tz-field-help'}>
              {intl.formatMessage({ id: touched && reasonBad ? 'common.reason.required' : 'common.reason.help' })}
            </p>
          </div>
        )}
        {touched && unchanged && (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'catalogue.error.unchanged' })}
          </p>
        )}
        {error && (
          <p role="alert" className="tz-field-error">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
