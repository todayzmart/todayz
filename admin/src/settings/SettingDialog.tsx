import type { AdminSetting } from '@todayz/api-client/gen/model/adminSetting';
import { ApiError } from '@todayz/api-client';
import { Button, Dialog, Segmented, TextField } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { MAX_REASON } from '../app/ReasonDialog';
import { useSettingLabels } from './labels';
import type { SettingChange } from './queries';
import { MAX_TEXT, inHundredths, parseInput, toInput, type ParseError } from './value';

export interface SettingDialogProps {
  setting: AdminSetting;
  pending: boolean;
  /** The last attempt's failure; a step-up request is handled by the page, not shown here. */
  error: unknown;
  onSave: (change: SettingChange) => void;
  onClose: () => void;
}

/** Change one setting: the new value in the setting's unit, and a reason that goes to the audit log. */
export function SettingDialog({ setting, pending, error, onSave, onClose }: SettingDialogProps) {
  const intl = useIntl();
  const id = useId();
  const labels = useSettingLabels();
  const problemText = useProblemText();
  const [raw, setRaw] = useState(() => (setting.unit === 'bool' ? String(setting.value === true) : toInput(setting.unit, setting.value)));
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const parsed = parseInput(setting.key, setting.unit, raw, setting);
  const valueError: ParseError | null = 'error' in parsed ? parsed.error : null;
  const clean = reason.trim();
  const reasonInvalid = clean.length === 0 || clean.length > MAX_REASON;
  const unchanged = 'value' in parsed && parsed.value === setting.value;
  const serverError = error && !(error instanceof ApiError && error.code === 'step_up_required') ? problemText(error) : null;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if ('error' in parsed || reasonInvalid || unchanged || pending) return;
    onSave({ key: setting.key, value: parsed.value, reason: clean });
  };

  const prefix = setting.unit === 'paise' ? '₹' : undefined;
  return (
    <Dialog
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'settings.edit.title' }, { key: setting.key })}
      description={setting.description}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'common.cancel' })}
          </Button>
          <Button type="submit" form={id} variant="primary" loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: 'settings.edit.save' })}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate>
        {setting.money && (
          <div role="note" className="tz-banner tz-banner-warning">
            {intl.formatMessage({ id: 'settings.edit.money' })}
          </div>
        )}
        <p className="tz-field-help">
          {intl.formatMessage(
            { id: 'settings.edit.current' },
            { value: labels.value(setting.unit, setting.value), range: labels.range(setting.unit, setting.min, setting.max) },
          )}
        </p>
        {setting.unit === 'bool' ? (
          <Segmented
            label={intl.formatMessage({ id: 'settings.edit.value' })}
            options={[
              { value: 'true', label: intl.formatMessage({ id: 'settings.value.on' }) },
              { value: 'false', label: intl.formatMessage({ id: 'settings.value.off' }) },
            ]}
            value={raw === 'true' ? 'true' : 'false'}
            onChange={setRaw}
          />
        ) : (
          <TextField
            label={intl.formatMessage({ id: setting.unit === 'bp' ? 'settings.edit.valuePercent' : 'settings.edit.value' })}
            prefix={prefix}
            inputMode={setting.unit === 'text' ? 'text' : inHundredths(setting.unit) ? 'decimal' : 'numeric'}
            maxLength={setting.unit === 'text' ? MAX_TEXT : 24}
            className={setting.unit === 'text' ? undefined : 'tz-num'}
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            help={labels.inputHelp(setting.unit)}
            error={
              touched && valueError
                ? labels.parseError(valueError, setting.unit, setting.min, setting.max)
                : touched && unchanged
                  ? intl.formatMessage({ id: 'settings.edit.error.same' })
                  : undefined
            }
          />
        )}
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
            aria-invalid={touched && reasonInvalid}
            aria-describedby={`${id}-help`}
            onChange={(e) => setReason(e.target.value)}
          />
          <p id={`${id}-help`} className={touched && reasonInvalid ? 'tz-field-error' : 'tz-field-help'}>
            {touched && reasonInvalid ? intl.formatMessage({ id: 'common.reason.required' }) : intl.formatMessage({ id: 'common.reason.help' })}
          </p>
        </div>
        {setting.unit === 'bool' && touched && unchanged && (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'settings.edit.error.same' })}
          </p>
        )}
        {serverError && (
          <p role="alert" className="tz-field-error">
            {serverError}
          </p>
        )}
      </form>
    </Dialog>
  );
}
