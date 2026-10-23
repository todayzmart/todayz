import { X } from 'lucide-react';
import { useEffect, useId, useLayoutEffect, useRef, type MouseEvent, type ReactNode } from 'react';
import { cx } from './cx';
import { Icon } from './Icon';

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
  /** Called on Escape, backdrop click or the close button. The parent sets open to false. */
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
  className?: string | undefined;
}

/**
 * Shared native `<dialog>` modal: showModal() gives the top layer, inert background and focus
 * containment for free; the browser returns focus to the opener on close.
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
  className,
}: ModalProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();
  const onCloseRef = useRef(onClose);
  useLayoutEffect(() => {
    onCloseRef.current = onClose;
  });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open) {
      if (!el.open) el.showModal();
      return lockScroll();
    }
    if (el.open) el.close();
    return undefined;
  }, [open]);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onCancel = (e: Event) => {
      e.preventDefault();
      if (dismissible) onCloseRef.current();
    };
    el.addEventListener('cancel', onCancel);
    return () => el.removeEventListener('cancel', onCancel);
  }, [dismissible]);

  const onBackdrop = (e: MouseEvent<HTMLDialogElement>) => {
    // A click on the dialog element itself (not its content) lands on the backdrop.
    if (dismissible && e.target === e.currentTarget) onClose();
  };

  return (
    // The dialog element is the backdrop hit area; keyboard users close with Escape.
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions
    <dialog
      ref={ref}
      className={className}
      role={role === 'alertdialog' ? 'alertdialog' : undefined}
      aria-labelledby={titleId}
      aria-describedby={description !== undefined ? descId : undefined}
      onClick={onBackdrop}
    >
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
    </dialog>
  );
}
