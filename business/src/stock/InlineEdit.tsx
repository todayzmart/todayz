import { Check, Pencil, X } from 'lucide-react';
import { IconButton } from '@todayz/ui';
import { useState, type ReactNode } from 'react';
import styles from './stock.module.css';

export interface InlineEditProps {
  /** Names the input and the buttons, e.g. "Price of Amul Butter". */
  label: string;
  /** The value as text when editing starts. */
  initial: string;
  /** Text to number, or null while it is not valid. */
  parse: (text: string) => number | null;
  /** Saves the parsed value; resolves true when it worked, false to stay open. */
  onSave: (value: number) => Promise<boolean>;
  /** What the cell shows when not editing. */
  display: ReactNode;
  /** false: just the display, no pencil. */
  editable: boolean;
  /** Always show the input (count mode), with a Confirm button. */
  always?: boolean;
  prefix?: string;
  invalidText: string;
  failedText?: string | undefined;
  saveLabel: string;
  cancelLabel: string;
  editLabel: string;
}

/**
 * A number edited in place: Enter saves, Escape cancels. The input is only mounted while editing (or
 * always, in count mode), so a table of hundreds of rows stays light.
 */
export function InlineEdit(p: InlineEditProps) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(p.initial);
  const [busy, setBusy] = useState(false);
  const [invalid, setInvalid] = useState(false);
  const [failed, setFailed] = useState(false);

  if (!p.editable) return <>{p.display}</>;

  const open = p.always || editing;
  if (!open) {
    return (
      <span className={styles.inline}>
        {p.display}
        <IconButton
          icon={Pencil}
          size="sm"
          variant="ghost"
          label={p.editLabel}
          onClick={() => {
            setText(p.initial);
            setInvalid(false);
            setFailed(false);
            setEditing(true);
          }}
        />
      </span>
    );
  }

  const save = async () => {
    const value = p.parse(text);
    if (value === null) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setFailed(false);
    setBusy(true);
    const ok = await p.onSave(value);
    setBusy(false);
    if (ok) setEditing(false);
    else setFailed(true);
  };

  const hint = invalid ? p.invalidText : failed ? p.failedText : undefined;
  return (
    <span className={styles.inline}>
      {p.prefix && <span aria-hidden="true">{p.prefix}</span>}
      <input
        className={`tz-input ${styles.inlineInput}`}
        inputMode="decimal"
        aria-label={p.label}
        aria-invalid={invalid || undefined}
        value={text}
        disabled={busy}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            void save();
          } else if (e.key === 'Escape' && !p.always) {
            setEditing(false);
          }
        }}
      />
      <IconButton icon={Check} size="sm" variant="soft" label={p.saveLabel} disabled={busy} onClick={() => void save()} />
      {!p.always && <IconButton icon={X} size="sm" variant="ghost" label={p.cancelLabel} disabled={busy} onClick={() => setEditing(false)} />}
      {hint && (
        <span role="alert" className="tz-field-error">
          {hint}
        </span>
      )}
    </span>
  );
}
