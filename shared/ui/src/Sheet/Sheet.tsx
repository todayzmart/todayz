import { Modal, type ModalProps } from '../internal/Modal';
import { cx } from '../internal/cx';

export type SheetProps = Omit<ModalProps, 'role'>;

/**
 * Bottom sheet: slides up from the bottom (220ms; a fade when motion is reduced), radius-xl top
 * corners, shadow-sheet, scrim behind. Escape, the scrim and the close button dismiss it.
 */
export function Sheet({ className, ...props }: SheetProps) {
  return <Modal {...props} className={cx('tz-ui-sheet', className)} />;
}
