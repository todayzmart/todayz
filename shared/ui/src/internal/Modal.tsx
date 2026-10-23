import { X } from 'lucide-react';
import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import { cx } from './cx';
import { Icon } from './Icon';
import { pushModal } from './modalStack';

let locks = 0;

/** Stops the page behind a modal from scrolling; nested modals share one lock. */
function lockScroll(): () => void {
  locks += 1;
  if (locks === 1) document.documentElement.classList.add('tz-ui-scroll-lock');
  return () => {
    locks -= 1;
    if (locks === 0) document.documentElement.classList.remove('tz-ui-scroll-lock');
  };
}

export interface ModalProps {
  open: boolean;
  /**
   * Called on Escape, backdrop click, the close button, or when the browser closes the dialog by
   * itself (a `<form method="dialog">` inside it). The parent sets open to false.
   */
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  /** Actions row at the bottom. */
  footer?: ReactNode;
  /** Close button name, e.g. "Close". Omit to hide the button (Escape still closes unless dismissible is false). */
  closeLabel?: string | undefined;
  /** false: Escape and backdrop do nothing (a decision the user must make with a footer button). */
  dismissible?: boolean | undefined;
  /** alertdialog for confirmations that interrupt. */
  role?: 'dialog' | 'alertdialog' | undefined;
  /**
   * Element focused when the modal opens, e.g. the safe button of a destructive confirmation.
   * Default: the browser's choice (the first focusable element), else the dialog itself.
   */
  initialFocusRef?: RefObject<HTMLElement | null> | undefined;
  className?: string | undefined;
}

/**
 * Shared native `<dialog>` modal: showModal() gives the top layer, inert background and focus
 * containment for free; the browser returns focus to the opener on close.
 *
 * `open` is the source of truth. When the browser closes the dialog by itself (form
 * method=dialog, or Chromium's forced close on a repeated Escape), the `close` event syncs it:
 * onClose is called, and if the parent keeps `open` true (or the modal is not dismissible and the
 * close came from Escape) the dialog opens again. Content mounts on the first open and unmounts
 * only after the dialog has closed, so focus goes back to the opener from inside the dialog.
 */
export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  closeLabel,
  dismissible = true,
  role = 'dialog',
  initialFocusRef,
  className,
}: ModalProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();
  const latest = useRef({ onClose, open, dismissible, initialFocusRef });
  useLayoutEffect(() => {
    latest.current = { onClose, open, dismissible, initialFocusRef };
  });
  /** Escape closed the dialog without a cancelable cancel event (Chromium's close watcher). */
  const forcedByEscape = useRef(false);
  /** The press that ends in a backdrop click also started on the backdrop (not a drag-select). */
  const downOnBackdrop = useRef(false);

  // Content is rendered from the first open until the dialog has actually closed.
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  // Bumped when the browser closes the dialog while `open` is still true, to re-sync below.
  const [nativeCloses, setNativeCloses] = useState(0);

  useEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    if (!open) {
      if (el.open) el.close();
      return undefined;
    }
    if (!el.open) {
      el.showModal();
      const target = latest.current.initialFocusRef?.current;
      if (target && el.contains(target)) target.focus();
      else if (!el.contains(document.activeElement)) el.focus();
    }
    const unlock = lockScroll();
    const unregister = pushModal(el);
    return () => {
      unregister();
      unlock();
    };
  }, [open, nativeCloses]);

  useEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    const onCancel = (e: Event) => {
      if (e.cancelable) {
        // The parent decides: Escape asks to close, the open prop closes.
        e.preventDefault();
      } else {
        // Not cancelable: the browser closes the dialog right after this event.
        forcedByEscape.current = true;
      }
      if (latest.current.dismissible) latest.current.onClose();
    };
    const onNativeClose = () => {
      const forced = forcedByEscape.current;
      forcedByEscape.current = false;
      setMounted(false);
      if (!latest.current.open) return;
      // Closed by the browser while the parent still has it open. A forced Escape already asked
      // to close in onCancel when dismissible; a non-dismissible modal opens again.
      if (!forced) latest.current.onClose();
      setNativeCloses((n) => n + 1);
    };
    el.addEventListener('cancel', onCancel);
    el.addEventListener('close', onNativeClose);
    return () => {
      el.removeEventListener('cancel', onCancel);
      el.removeEventListener('close', onNativeClose);
    };
  }, []);

  const onPointerDown = (e: PointerEvent<HTMLDialogElement>) => {
    downOnBackdrop.current = e.target === e.currentTarget;
  };
  const onBackdrop = (e: MouseEvent<HTMLDialogElement>) => {
    // A click on the dialog element itself (not its content) lands on the backdrop. A drag that
    // starts in the content (selecting text) and ends on the backdrop also clicks the dialog, so
    // the press must have started there too.
    const fromBackdrop = downOnBackdrop.current;
    downOnBackdrop.current = false;
    if (dismissible && fromBackdrop && e.target === e.currentTarget) onClose();
  };

  const body = open || mounted;
  return (
    // The dialog element is the backdrop hit area; keyboard users close with Escape.
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions
    <dialog
      ref={ref}
      className={className}
      role={role === 'alertdialog' ? 'alertdialog' : undefined}
      aria-labelledby={body ? titleId : undefined}
      aria-describedby={body && description !== undefined ? descId : undefined}
      // Focus target when nothing inside can take focus.
      tabIndex={-1}
      onPointerDown={onPointerDown}
      onClick={onBackdrop}
    >
      {body && (
        <div className="tz-ui-modal-body">
          <div className="tz-ui-modal-head">
            <h2 className="tz-ui-modal-title" id={titleId}>
              {title}
            </h2>
            {closeLabel !== undefined && (
              <button
                type="button"
                className="tz-btn tz-btn-icon tz-btn-sm tz-btn-ghost"
                aria-label={closeLabel}
                onClick={onClose}
              >
                <Icon icon={X} />
              </button>
            )}
          </div>
          {description !== undefined && (
            <p className="tz-ui-modal-desc" id={descId}>
              {description}
            </p>
          )}
          {children}
          {footer !== undefined && <div className={cx('tz-ui-modal-foot')}>{footer}</div>}
        </div>
      )}
    </dialog>
  );
}
