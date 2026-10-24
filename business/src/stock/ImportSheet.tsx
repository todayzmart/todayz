import { formatMoney } from '@todayz/i18n';
import { Banner, Button, Sheet, StatusPill } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useIdempotentAction } from './action';
import { applyItemImport, uploadItemImport, type ImportApplied, type ImportPreview, type ImportRowError } from './api';
import { stockKey } from './queries';
import { StockError } from './StockError';
import styles from './stock.module.css';

/** The largest file the screen sends; the server checks again. */
const MAX_BYTES = 5 * 1024 * 1024;

const FIELD_IDS: Record<string, string> = {
  name: 'stock.import.field.name',
  barcode: 'stock.import.field.barcode',
  pricePaise: 'stock.import.field.pricePaise',
  mrpPaise: 'stock.import.field.mrpPaise',
  quantity: 'stock.import.field.quantity',
  expiresOn: 'stock.import.field.expiresOn',
};
const CODE_IDS: Record<string, string> = {
  required: 'stock.import.code.required',
  invalid: 'stock.import.code.invalid',
  too_long: 'stock.import.code.too_long',
  price_above_mrp: 'stock.import.code.price_above_mrp',
  unknown_product: 'stock.import.code.unknown_product',
  duplicate: 'stock.import.code.duplicate',
};

function errorText(intl: IntlShape, e: ImportRowError): string {
  return intl.formatMessage(
    { id: 'stock.import.rowError' },
    {
      field: intl.formatMessage({ id: FIELD_IDS[e.field] ?? 'stock.import.field.other' }),
      problem: intl.formatMessage({ id: CODE_IDS[e.code] ?? 'stock.import.code.other' }),
    },
  );
}

/** Bulk import: upload, read the row-by-row preview, then apply only the rows without errors. */
export function ImportSheet({ store, onClose }: { store: CurrentStore; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const upload = useIdempotentAction();
  const apply = useIdempotentAction();
  const input = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [done, setDone] = useState<ImportApplied | null>(null);
  const [tooBig, setTooBig] = useState(false);

  const pick = async (file: File | undefined) => {
    if (!file) return;
    setTooBig(file.size > MAX_BYTES);
    if (file.size > MAX_BYTES) return;
    const out = await upload.run(`${file.name}:${file.size}:${file.lastModified}`, () => uploadItemImport(store.bid, store.sid, file));
    if (out) setPreview(out);
  };

  const confirm = async () => {
    if (!preview) return;
    const out = await apply.run(preview.id, (key) => applyItemImport(store.bid, store.sid, preview.id, key));
    if (out) {
      setDone(out);
      void qc.invalidateQueries({ queryKey: stockKey(store.sid) });
    }
  };

  return (
    <Sheet open onClose={onClose} title={intl.formatMessage({ id: 'stock.import.title' })} closeLabel={intl.formatMessage({ id: 'stock.item.close' })}>
      <div className={styles.sheetBody}>
        {done ? (
          <>
            <Banner live="status">{intl.formatMessage({ id: 'stock.import.done' }, { applied: done.applied, skipped: done.skipped })}</Banner>
            <div className={styles.formActions}>
              <Button variant="primary" onClick={onClose}>
                {intl.formatMessage({ id: 'stock.import.close' })}
              </Button>
            </div>
          </>
        ) : preview ? (
          <>
            <p className={styles.sub}>{preview.fileName}</p>
            <div className={styles.importSummary}>
              <StatusPill tone="ready">{intl.formatMessage({ id: 'stock.import.okCount' }, { count: preview.counts.ok })}</StatusPill>
              {preview.counts.error > 0 && (
                <StatusPill tone="rejected">{intl.formatMessage({ id: 'stock.import.errorCount' }, { count: preview.counts.error })}</StatusPill>
              )}
            </div>
            {preview.counts.error > 0 && <p className={styles.note}>{intl.formatMessage({ id: 'stock.import.errorNote' })}</p>}
            <ul className={styles.rows} aria-label={intl.formatMessage({ id: 'stock.import.rows' })}>
              {preview.rows.map((r) => (
                <li key={r.row} className={styles.row}>
                  <span className={styles.cell}>
                    <span>
                      {intl.formatMessage({ id: 'stock.import.rowNo' }, { row: r.row })}
                      {' · '}
                      {r.name ?? r.barcode ?? intl.formatMessage({ id: 'stock.import.unnamed' })}
                    </span>
                    {r.errors.map((e) => (
                      <span key={`${e.field}:${e.code}`} className={styles.rowError}>
                        {errorText(intl, e)}
                      </span>
                    ))}
                  </span>
                  <span className={styles.sub}>
                    {r.pricePaise !== null && formatMoney(r.pricePaise, 'product')}
                    {r.quantity !== null && ` · ×${r.quantity}`}
                  </span>
                </li>
              ))}
            </ul>
            <StockError error={apply.error} />
            <div className={styles.formActions}>
              <Button onClick={() => setPreview(null)}>{intl.formatMessage({ id: 'stock.import.another' })}</Button>
              <Button variant="primary" disabled={preview.counts.ok === 0} loading={apply.pending} onClick={() => void confirm()}>
                {intl.formatMessage({ id: 'stock.import.apply' }, { count: preview.counts.ok })}
              </Button>
            </div>
          </>
        ) : (
          <>
            <p>{intl.formatMessage({ id: 'stock.import.body' })}</p>
            <input
              ref={input}
              type="file"
              accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              className="tz-ui-sr-only"
              aria-label={intl.formatMessage({ id: 'stock.import.file' })}
              onChange={(e) => void pick(e.target.files?.[0])}
            />
            <div className={styles.actions}>
              <Button variant="primary" loading={upload.pending} onClick={() => input.current?.click()}>
                {intl.formatMessage({ id: 'stock.import.choose' })}
              </Button>
            </div>
            {tooBig && (
              <p role="alert" className="tz-field-error">
                {intl.formatMessage({ id: 'stock.import.tooBig' })}
              </p>
            )}
            <StockError error={upload.error} />
          </>
        )}
      </div>
    </Sheet>
  );
}
