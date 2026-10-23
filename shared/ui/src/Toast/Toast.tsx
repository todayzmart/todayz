import { CircleCheck } from 'lucide-react';
import {
  forwardRef,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type FocusEvent,
  type HTMLAttributes,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import { subscribeModals, topModal } from '../internal/modalStack';
import { ToastContext, type ShowToast, type ToastEntry, type ToastProps } from './context';

export interface ToastElementProps extends ToastProps, Omit<HTMLAttributes<HTMLSpanElement>, 'children'> {
  /** Router link for `href` actions (client-side navigation); the default is `<a>`. */
  LinkComponent?: LinkComponent | undefined;
}

/** One toast (tz-toast): inverse pill, optional icon and one underlined action (a link or a button). */
export const Toast = forwardRef<HTMLSpanElement, ToastElementProps>(function Toast(
  { message, icon = CircleCheck, action, className, LinkComponent, ...rest },
  ref,
) {
  const Link = LinkComponent ?? 'a';
  return (
    <span ref={ref} className={cx('tz-toast', className)} {...rest}>
      <Icon icon={icon} />
      {message}{' '}
      {action &&
        (action.href !== undefined ? (
          <Link href={action.href} className="tz-ui-toast-action" onClick={action.onClick}>
            {action.label}
          </Link>
        ) : (
          <button type="button" className="tz-ui-toast-action" onClick={action.onClick}>
            {action.label}
          </button>
        ))}
    </span>
  );
});

export interface ToastProviderProps {
  children: ReactNode;
  /** Default time on screen in ms for a toast without an action (4000). */
  duration?: number | undefined;
  /**
   * Default time on screen in ms for a toast with an action (10000), long enough to reach it by
   * keyboard. Pass `duration: null` on a toast whose action exists nowhere else, so it stays until
   * used or dismissed (WCAG 2.2.1).
   */
  actionDuration?: number | undefined;
  /** Most toasts on screen; the oldest goes first. */
  max?: number | undefined;
  /** Router link for `href` actions; the default is `<a>`. */
  LinkComponent?: LinkComponent | undefined;
}

const noModal = () => null;

/**
 * Toast region: a polite live region pinned above the bottom nav (z-toast). Toasts never steal
 * focus; actions are reachable by Tab. Every timer pauses while the pointer or focus is anywhere
 * in the region. While a Dialog or Sheet is open the region renders inside it: a modal dialog
 * makes everything outside it inert, even other top-layer elements.
 */
export function ToastProvider({
  children,
  duration = 4000,
  actionDuration = 10_000,
  max = 3,
  LinkComponent,
}: ToastProviderProps) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const nextId = useRef(1);
  const host = useSyncExternalStore(subscribeModals, topModal, noModal);

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

  // The last toast going away under the pointer or with focus (its action dismissed it) fires no
  // mouseleave or blur, so an empty region resets the pause for the next toast.
  if (toasts.length === 0 && (hovered || focused)) {
    setHovered(false);
    setFocused(false);
  }

  const onBlur = (e: FocusEvent<HTMLDivElement>) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocused(false);
  };

  const region = (
    // Hover and focus only pause the timers; the region itself is not a control.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      className="tz-ui-toast-region"
      role="status"
      aria-live="polite"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocus={() => setFocused(true)}
      onBlur={onBlur}
    >
      {toasts.map((t) => (
        <TimedToast
          key={t.id}
          entry={t}
          fallback={t.action ? actionDuration : duration}
          paused={hovered || focused}
          onDone={dismiss}
          LinkComponent={LinkComponent}
        />
      ))}
    </div>
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      {host ? createPortal(region, host) : region}
    </ToastContext.Provider>
  );
}

function TimedToast({
  entry,
  fallback,
  paused,
  onDone,
  LinkComponent,
}: {
  entry: ToastEntry;
  fallback: number;
  paused: boolean;
  onDone: (id: number) => void;
  LinkComponent: LinkComponent | undefined;
}) {
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
    <div className="tz-ui-toast-item">
      <Toast
        message={entry.message}
        icon={entry.icon}
        action={action}
        className={entry.className}
        LinkComponent={LinkComponent}
      />
    </div>
  );
}
