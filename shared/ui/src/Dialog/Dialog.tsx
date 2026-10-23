import { Modal, type ModalProps } from '../internal/Modal';
import { cx } from '../internal/cx';

export type DialogProps = ModalProps;

/**
 * Centred dialog for confirmations and short forms. Use role="alertdialog" with
 * dismissible={false} when the user must choose (e.g. "Cancel this order?").
 */
export function Dialog({ className, ...props }: DialogProps) {
  return <Modal {...props} className={cx('tz-ui-dialog', className)} />;
}
