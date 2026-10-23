import { useId, useRef, type KeyboardEvent, type ReactNode } from 'react';
import { cx } from '../internal/cx';

export interface TabItem<V extends string> {
  value: V;
  label: ReactNode;
  /** Small count after the label (e.g. orders waiting). */
  count?: number | undefined;
  panel?: ReactNode;
}

export interface TabsProps<V extends string> {
  /** Accessible name of the tab list. */
  label: string;
  tabs: readonly TabItem<V>[];
  value: V;
  onChange: (value: V) => void;
  className?: string | undefined;
}

/**
 * WAI-ARIA tabs with automatic activation: arrows, Home and End move and select. The selected tab
 * has a 2px ink underline. Panels render only for the selected tab.
 */
export function Tabs<V extends string>({ label, tabs, value, onChange, className }: TabsProps<V>) {
  const base = useId();
  const listRef = useRef<HTMLDivElement>(null);
  const tabId = (v: V) => `${base}-tab-${v}`;
  const panelId = (v: V) => `${base}-panel-${v}`;

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const at = tabs.findIndex((t) => t.value === value);
    let next = -1;
    if (e.key === 'ArrowRight') next = (at + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (at - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    const tab = tabs[next];
    if (!tab) return;
    e.preventDefault();
    onChange(tab.value);
    listRef.current?.querySelector<HTMLButtonElement>(`#${CSS.escape(tabId(tab.value))}`)?.focus();
  };

  const selected = tabs.find((t) => t.value === value);
  return (
    <div className={cx('tz-ui-tabs', className)}>
      <div ref={listRef} role="tablist" aria-label={label} className="tz-ui-tablist">
        {tabs.map((t) => {
          const on = t.value === value;
          return (
            <button
              key={t.value}
              id={tabId(t.value)}
              type="button"
              role="tab"
              aria-selected={on}
              aria-controls={t.panel !== undefined ? panelId(t.value) : undefined}
              tabIndex={on ? 0 : -1}
              className="tz-ui-tab"
              onClick={() => onChange(t.value)}
              onKeyDown={onKeyDown}
            >
              {t.label}
              {t.count !== undefined && <span className="tz-ui-tab-count tz-num">{t.count}</span>}
            </button>
          );
        })}
      </div>
      {selected?.panel !== undefined && (
        // WAI-ARIA tabs pattern: the panel takes focus when it holds no focusable content.
        <div
          role="tabpanel"
          id={panelId(selected.value)}
          aria-labelledby={tabId(selected.value)}
          // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
          tabIndex={0}
          className="tz-ui-tabpanel"
        >
          {selected.panel}
        </div>
      )}
    </div>
  );
}
