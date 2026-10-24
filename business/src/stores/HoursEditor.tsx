import type { HoursDay } from '@todayz/api-client/gen/model/hoursDay';
import type { StoreHours } from '@todayz/api-client/gen/model/storeHours';
import { Button, IconButton, TextField } from '@todayz/ui';
import { Plus, Trash2 } from 'lucide-react';
import { useIntl } from 'react-intl';
import {
  ALL_DAY,
  DAYS,
  MAX_HOLIDAYS,
  MAX_RANGES,
  copyFirstDay,
  dayOf,
  isAllDay,
  type Day,
  type HoursErrors,
} from './hours';
import styles from './stores.module.css';

export interface HoursEditorProps {
  value: StoreHours;
  onChange: (next: StoreHours) => void;
  errors: HoursErrors;
  /** Show the errors (after the first Save). */
  showErrors: boolean;
  /** The earliest holiday date that may be picked (today, IST), `YYYY-MM-DD`. */
  minDate?: string | undefined;
}

const NEW_RANGE = { open: '09:00', close: '21:00' };

/** Weekly opening hours (up to four ranges a day, 24 hours, closed) and holidays. Used by B-02 and B-14. */
export function HoursEditor({ value, onChange, errors, showErrors, minDate }: HoursEditorProps) {
  const intl = useIntl();
  const t = (id: string, values?: Record<string, string | number>) => intl.formatMessage({ id }, values);

  const setDay = (d: Day, ranges: HoursDay) => onChange({ ...value, weekly: { ...value.weekly, [d]: ranges } });

  return (
    <div className={styles.stack}>
      <div>
        {DAYS.map((d) => {
          const ranges = dayOf(value, d);
          const open = ranges.length > 0;
          const allDay = isAllDay(ranges);
          const err = showErrors ? errors[d] : undefined;
          return (
            <div key={d} className={styles.day} role="group" aria-label={t(`stores.hours.day.${d}`)}>
              <div className={styles.dayHead}>
                <span className={styles.dayName}>{t(`stores.hours.day.${d}`)}</span>
                <label className={styles.check}>
                  <input type="checkbox" checked={open} onChange={(e) => setDay(d, e.target.checked ? [{ ...NEW_RANGE }] : [])} />
                  <span>{open ? t('stores.hours.open') : t('stores.hours.closed')}</span>
                </label>
              </div>
              {open && (
                <>
                  <label className={styles.check}>
                    <input type="checkbox" checked={allDay} onChange={(e) => setDay(d, e.target.checked ? ALL_DAY.map((r) => ({ ...r })) : [{ ...NEW_RANGE }])} />
                    <span>{t('stores.hours.allDay')}</span>
                  </label>
                  {!allDay && (
                    <div className={styles.ranges}>
                      {ranges.map((r, i) => (
                        <div key={i} className={styles.range}>
                          <TextField
                            type="time"
                            label={t('stores.hours.from')}
                            fieldClassName={styles.time}
                            value={r.open}
                            onChange={(e) => setDay(d, ranges.map((x, j) => (j === i ? { ...x, open: e.target.value } : x)))}
                          />
                          <TextField
                            type="time"
                            label={t('stores.hours.to')}
                            fieldClassName={styles.time}
                            value={r.close === '24:00' ? '00:00' : r.close}
                            onChange={(e) => setDay(d, ranges.map((x, j) => (j === i ? { ...x, close: e.target.value } : x)))}
                          />
                          {ranges.length > 1 && (
                            <IconButton
                              icon={Trash2}
                              label={t('stores.hours.removeRange', { day: t(`stores.hours.day.${d}`) })}
                              onClick={() => setDay(d, ranges.filter((_, j) => j !== i))}
                            />
                          )}
                        </div>
                      ))}
                      {ranges.length < MAX_RANGES && (
                        <div className={styles.actions}>
                          <Button icon={Plus} onClick={() => setDay(d, [...ranges, { open: '16:00', close: '21:00' }])}>
                            {t('stores.hours.addRange')}
                          </Button>
                        </div>
                      )}
                    </div>
                  )}
                </>
              )}
              {err && (
                <p role="alert" className="tz-field-error">
                  {t(`stores.hours.err.${err}`)}
                </p>
              )}
            </div>
          );
        })}
      </div>
      <div className={styles.actions}>
        <Button onClick={() => onChange(copyFirstDay(value))}>{t('stores.hours.copyMonday')}</Button>
      </div>

      <fieldset className={styles.fieldset}>
        <legend className={styles.legend}>{t('stores.hours.holidays')}</legend>
        <p className={styles.note}>{t('stores.hours.holidaysHelp')}</p>
        {value.holidays.map((h, i) => {
          const err = showErrors ? errors.holidays?.[i] : undefined;
          const closed = h.ranges.length === 0;
          return (
            <div key={i} className={styles.holiday}>
              <div className={styles.range}>
                <TextField
                  type="date"
                  label={t('stores.hours.holidayDate')}
                  value={h.date}
                  min={minDate}
                  onChange={(e) => onChange({ ...value, holidays: value.holidays.map((x, j) => (j === i ? { ...x, date: e.target.value } : x)) })}
                />
                <IconButton
                  icon={Trash2}
                  label={t('stores.hours.removeHoliday', { date: h.date || String(i + 1) })}
                  onClick={() => onChange({ ...value, holidays: value.holidays.filter((_, j) => j !== i) })}
                />
              </div>
              <label className={styles.check}>
                <input
                  type="checkbox"
                  checked={closed}
                  onChange={(e) =>
                    onChange({
                      ...value,
                      holidays: value.holidays.map((x, j) => (j === i ? { ...x, ranges: e.target.checked ? [] : [{ ...NEW_RANGE }] } : x)),
                    })
                  }
                />
                <span>{t('stores.hours.holidayClosed')}</span>
              </label>
              {!closed && (
                <div className={styles.range}>
                  <TextField
                    type="time"
                    label={t('stores.hours.from')}
                    fieldClassName={styles.time}
                    value={h.ranges[0]?.open ?? ''}
                    onChange={(e) =>
                      onChange({ ...value, holidays: value.holidays.map((x, j) => (j === i ? { ...x, ranges: [{ open: e.target.value, close: x.ranges[0]?.close ?? '21:00' }] } : x)) })
                    }
                  />
                  <TextField
                    type="time"
                    label={t('stores.hours.to')}
                    fieldClassName={styles.time}
                    value={h.ranges[0]?.close === '24:00' ? '00:00' : (h.ranges[0]?.close ?? '')}
                    onChange={(e) =>
                      onChange({ ...value, holidays: value.holidays.map((x, j) => (j === i ? { ...x, ranges: [{ open: x.ranges[0]?.open ?? '09:00', close: e.target.value }] } : x)) })
                    }
                  />
                </div>
              )}
              {err && (
                <p role="alert" className="tz-field-error">
                  {t(`stores.hours.err.${err}`)}
                </p>
              )}
            </div>
          );
        })}
        {value.holidays.length < MAX_HOLIDAYS && (
          <div className={styles.actions}>
            <Button icon={Plus} onClick={() => onChange({ ...value, holidays: [...value.holidays, { date: '', ranges: [] }] })}>
              {t('stores.hours.addHoliday')}
            </Button>
          </div>
        )}
      </fieldset>
    </div>
  );
}
