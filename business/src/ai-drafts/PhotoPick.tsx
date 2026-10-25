import { Camera } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { Icon } from '../shell/Icon';
import { checkPhoto } from './photo';
import styles from './ai.module.css';

/**
 * One photo from the camera or the gallery. The file is checked here first, so a wrong type or a
 * photo above the limit is a note on the screen, never an upload; the server checks again.
 */
export function PhotoPick({ label, types, maxBytes, busy, onPick }: { label: string; types: readonly string[]; maxBytes: number; busy: boolean; onPick: (file: File) => void }) {
  const intl = useIntl();
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [bad, setBad] = useState<'type' | 'size' | null>(null);
  return (
    <div className={styles.pick}>
      <label htmlFor={id} className="tz-btn tz-btn-primary" aria-disabled={busy || undefined} data-busy={busy || undefined}>
        <Icon icon={Camera} />
        {label}
      </label>
      <input
        ref={input}
        id={id}
        className={styles.file}
        type="file"
        accept={types.join(',')}
        capture="environment"
        disabled={busy}
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (input.current) input.current.value = '';
          if (!file) return;
          const why = checkPhoto(file, types, maxBytes);
          setBad(why);
          if (why === null) onPick(file);
        }}
      />
      <span className="tz-field-help">{intl.formatMessage({ id: 'ai.photo.help' }, { mb: Math.floor(maxBytes / (1024 * 1024)) })}</span>
      {bad && (
        <span role="alert" className="tz-field-error">
          {intl.formatMessage({ id: `ai.photo.${bad}` }, { mb: Math.floor(maxBytes / (1024 * 1024)) })}
        </span>
      )}
    </div>
  );
}
