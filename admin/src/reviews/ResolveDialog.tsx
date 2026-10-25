import type { ResolveReportRequest } from '@todayz/api-client/gen/model/resolveReportRequest';
import type { ResolveReportRequestResolution } from '@todayz/api-client/gen/model/resolveReportRequestResolution';
import { Button, Dialog, Segmented } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import css from '../app/page.module.css';

/** The contract bound on ResolveReportRequest.note. */
export const MAX_NOTE = 500;

export interface ResolveDialogProps {
  open: boolean;
  pending: boolean;
  error: string | null;
  onConfirm: (body: ResolveReportRequest) => void;
  onClose: () => void;
}

/**
 * Close a report (01 §10): dismissed leaves the review as it is, upheld closes the report as justified.
 * Neither hides the review; that is its own move with its own reason. No choice is made for the moderator.
 */
export function ResolveDialog({ open, pending, error, onConfirm, onClose }: ResolveDialogProps) {
  const intl = useIntl();
  const id = useId();
  const [resolution, setResolution] = useState<ResolveReportRequestResolution | ''>('');
  const [note, setNote] = useState('');
  const [touched, setTouched] = useState(false);
  const clean = note.trim();
  const missing = resolution === '';
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (resolution === '' || pending) return;
    onConfirm({ resolution, ...(clean ? { note: clean } : {}) });
  };
  const close = () => {
    setResolution('');
    setNote('');
    setTouched(false);
    onClose();
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      title={intl.formatMessage({ id: 'reviews.resolve.title' })}
      description={intl.formatMessage({ id: 'reviews.resolve.body' })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            {intl.formatMessage({ id: 'reviews.dialog.keep' })}
          </Button>
          <Button type="submit" form={id} variant="primary" loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: 'reviews.resolve.confirm' })}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate className={css.stack}>
        <div className="tz-field">
          <Segmented<ResolveReportRequestResolution | ''>
            label={intl.formatMessage({ id: 'reviews.resolve.decision' })}
            options={[
              { value: 'dismissed', label: intl.formatMessage({ id: 'reviews.resolution.dismissed' }) },
              { value: 'upheld', label: intl.formatMessage({ id: 'reviews.resolution.upheld' }) },
            ]}
            value={resolution}
            onChange={setResolution}
          />
          {touched && missing && <p className="tz-field-error">{intl.formatMessage({ id: 'reviews.resolve.decisionRequired' })}</p>}
        </div>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-note`}>
            {intl.formatMessage({ id: 'reviews.resolve.note' })}
          </label>
          <textarea id={`${id}-note`} className="tz-input" rows={2} maxLength={MAX_NOTE} value={note} onChange={(e) => setNote(e.target.value)} />
          <p className="tz-field-help">{intl.formatMessage({ id: 'reviews.resolve.noteHelp' }, { max: MAX_NOTE })}</p>
        </div>
        {error && (
          <p role="alert" className="tz-field-error">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
