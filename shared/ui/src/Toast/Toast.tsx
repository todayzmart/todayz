import { CircleCheck } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import { ToastContext, type ShowToast, type ToastEntry, type ToastProps } from './context';

/** One toast (tz-toast): inverse pill, optional icon and one underlined action. */
export function Toast({ message, icon = CircleCheck, action, className }: ToastProps) {
  return (
    <span className={cx('tz-toast', className)}>
      <Icon icon={icon} />
      {message}{' '}
      {action &&
        (action.href !== undefined ? (
          <a href={action.href} onClick={action.onClick}>
            {action.label}
          </a>
        ) : (
          <button type="button" className="tz-ui-toast-action" onClick={action.onClick}>
            {action.label}
          </button>
        ))}
    </span>
  );
}

export interface ToastProviderProps {
  children: ReactNode;
  /** Default time on screen in ms (4000). Hover or focus pauses it. */
  duration?: number | undefined;
  /** Most toasts on screen; the oldest goes first. */
  max?: number | undefined;
}

/**
 * Toast region: a polite live region pinned above the bottom nav (z-toast). Toasts never steal
 * focus; actions are reachable by Tab.
 */
export function ToastProvider({ children, duration = 4000, max = 3 }: ToastProviderProps) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => setToasts((t) => t.filter((x) => x.id !== id)), []);
  const show = useCallback(
    (toast: ShowToast) => {
      const id = nextId.current++;
      setToasts((t) => [...t, { ...toast, id }].slice(-max));
      return id;
    },
    [max],
  );
  const api = useMemo(() => ({ show, dismiss }), [show, dismiss]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="tz-ui-toast-region" role="status" aria-live="polite">
        {toasts.map((t) => (
          <TimedToast key={t.id} entry={t} fallback={duration} onDone={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function TimedToast({
  entry,
  fallback,
  onDone,
}: {
  entry: ToastEntry;
  fallback: number;
  onDone: (id: number) => void;
}) {
  const [paused, setPaused] = useState(false);
  const ms = entry.duration === undefined ? fallback : entry.duration;
  useEffect(() => {
    if (ms === null || paused) return undefined;
    const t = setTimeout(() => onDone(entry.id), ms);
    return () => clearTimeout(t);
  }, [ms, paused, entry.id, onDone]);
  const action = entry.action && {
    ...entry.action,
    onClick: () => {
      entry.action?.onClick?.();
      onDone(entry.id);
    },
  };
  return (
    // Hover and focus only pause the timer; the toast itself is not a control.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions
    <div
      className="tz-ui-toast-item"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      <Toast message={entry.message} icon={entry.icon} action={action} className={entry.className} />
    </div>
  );
}
