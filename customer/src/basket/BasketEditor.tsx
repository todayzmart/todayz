// C-22 editor: the "just ask" box (a product from the suggestions, or the words as typed), the lines with a
// stepper and remove, and the name. The same editor serves a saved basket and the device basket.
import { searchSuggest } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverSuggestion } from '@todayz/api-client/gen/model/discoverSuggestion';
import { useQuery } from '@tanstack/react-query';
import { Button, QuantityStepper, SearchField, TextField } from '@todayz/ui';
import { Package, Trash2, Type } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { Icon } from '../shell/Icon';
import { useStepperLabels } from '../store/itemUi';
import { addLine, NAME_MAX, QTY_MAX, QTY_MIN, type EditLine } from './model';
import styles from './basket.module.css';

/** The words the box cycles through ("Add atta, milk, bread…"). Examples, not data. */
const EXAMPLES = ['basket.ask.example1', 'basket.ask.example2', 'basket.ask.example3', 'basket.ask.example4'];
const TEXT_MAX = 120;

function reducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export interface BasketEditorProps {
  name: string;
  lines: readonly EditLine[];
  onName: (name: string) => void;
  onLines: (lines: EditLine[]) => void;
  /** Shown under the name, e.g. "Saving…" or "Saved". */
  status?: string | undefined;
}

export function BasketEditor({ name, lines, onName, onLines, status }: BasketEditorProps) {
  const intl = useIntl();
  const stepperLabels = useStepperLabels();
  const [draft, setDraft] = useState('');
  const [typed, setTyped] = useState('');
  const [nameDraft, setNameDraft] = useState(name);
  const [seenName, setSeenName] = useState(name);
  if (seenName !== name) {
    setSeenName(name);
    setNameDraft(name);
  }

  const [example, setExample] = useState(0);
  useEffect(() => {
    if (reducedMotion()) return;
    const t = window.setInterval(() => setExample((i) => (i + 1) % EXAMPLES.length), 3500);
    return () => window.clearInterval(t);
  }, []);

  const suggest = useQuery({
    queryKey: ['discover', 'suggest', typed],
    enabled: typed.length >= 2,
    staleTime: 5 * 60_000,
    queryFn: () => searchSuggest({ q: typed, limit: 6 }),
  });

  const reset = () => {
    setDraft('');
    setTyped('');
  };
  const addText = (text: string) => {
    const t = text.trim().slice(0, TEXT_MAX);
    if (!t) return;
    onLines(addLine(lines, { productId: null, text: t, label: t, detail: '', quantity: 1 }));
    reset();
  };
  const addSuggestion = (s: DiscoverSuggestion) => {
    if (s.kind === 'product' && s.productId) {
      onLines(addLine(lines, { productId: s.productId, text: null, label: s.text, detail: '', quantity: 1 }));
      reset();
    } else {
      addText(s.text);
    }
  };
  const setQty = (key: string, quantity: number) => {
    if (quantity < QTY_MIN) {
      onLines(lines.filter((l) => l.key !== key));
      return;
    }
    onLines(lines.map((l) => (l.key === key ? { ...l, quantity: Math.min(QTY_MAX, quantity) } : l)));
  };

  const suggestions = typed.length >= 2 && draft.trim() ? (suggest.data?.items ?? []) : [];

  return (
    <div className={styles.editor}>
      <form
        role="search"
        className={styles.ask}
        onSubmit={(e) => {
          e.preventDefault();
          addText(draft);
        }}
      >
        <SearchField
          label={intl.formatMessage({ id: 'basket.ask.label' })}
          placeholder={intl.formatMessage({ id: 'basket.ask.placeholder' })}
          clearLabel={intl.formatMessage({ id: 'basket.ask.clear' })}
          value={draft}
          onChange={setDraft}
          onSearch={setTyped}
          enterKeyHint="done"
          maxLength={TEXT_MAX}
        />
        <p className={styles.example} aria-hidden="true">
          {intl.formatMessage({ id: 'basket.ask.try' }, { example: intl.formatMessage({ id: EXAMPLES[example] ?? 'basket.ask.example1' }) })}
        </p>
        {draft.trim() && (
          <ul className={styles.suggestions} aria-label={intl.formatMessage({ id: 'basket.ask.suggestions' })}>
            {suggestions.map((s, i) => (
              <li key={`${s.kind}-${s.text}-${i}`}>
                <button type="button" className={styles.suggestion} onClick={() => addSuggestion(s)}>
                  <Icon icon={s.kind === 'product' ? Package : Type} />
                  <span>{s.text}</span>
                </button>
              </li>
            ))}
            <li>
              <button type="button" className={styles.suggestion} onClick={() => addText(draft)}>
                <Icon icon={Type} />
                <span>{intl.formatMessage({ id: 'basket.ask.addText' }, { text: draft.trim() })}</span>
              </button>
            </li>
          </ul>
        )}
      </form>

      {lines.length === 0 ? (
        <p className={styles.hint}>{intl.formatMessage({ id: 'basket.lines.empty' })}</p>
      ) : (
        <ul className={styles.lines} aria-label={intl.formatMessage({ id: 'basket.lines.label' })}>
          {lines.map((l) => (
            <li key={l.key} className={styles.line}>
              <div className={styles.lineText}>
                <span className={styles.lineName}>{l.label}</span>
                {l.detail ? <span className="tz-meta">{l.detail}</span> : null}
                {!l.productId ? <span className="tz-meta">{intl.formatMessage({ id: 'basket.lines.anyBrand' })}</span> : null}
              </div>
              <QuantityStepper
                quantity={l.quantity}
                onChange={(q) => setQty(l.key, q)}
                max={QTY_MAX}
                alwaysStepper
                size="sm"
                itemName={l.label}
                labels={stepperLabels}
              />
              <Button
                variant="ghost"
                size="sm"
                icon={Trash2}
                aria-label={intl.formatMessage({ id: 'basket.lines.remove' }, { name: l.label })}
                onClick={() => onLines(lines.filter((x) => x.key !== l.key))}
              />
            </li>
          ))}
        </ul>
      )}

      <TextField
        label={intl.formatMessage({ id: 'basket.name.label' })}
        help={status}
        value={nameDraft}
        maxLength={NAME_MAX}
        placeholder={intl.formatMessage({ id: 'basket.name.placeholder' })}
        onChange={(e) => setNameDraft(e.target.value)}
        onBlur={() => {
          const n = nameDraft.trim().slice(0, NAME_MAX);
          if (n && n !== name) onName(n);
          else setNameDraft(name);
        }}
      />
    </div>
  );
}
