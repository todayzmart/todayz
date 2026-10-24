import { Button, Sheet } from '@todayz/ui';
import { MessageCircle, MessageSquare, Printer } from 'lucide-react';
import { useIntl } from 'react-intl';
import { receiptUrl, smsShareUrl, whatsappShareUrl } from '../pos/links';
import type { SaleRecord } from '../pos/types';
import { ReceiptView } from './ReceiptView';
import styles from './receipt.module.css';

export interface ReceiptSheetProps {
  open: boolean;
  onClose: () => void;
  shopName: string;
  record: SaleRecord | null;
}

/** Receipt with its three actions: WhatsApp link, SMS link, print (B-05). */
export function ReceiptSheet({ open, onClose, shopName, record }: ReceiptSheetProps) {
  const intl = useIntl();
  const token = record?.receiptToken ?? null;
  const phone = record?.sale.link && record.sale.link.method !== 'bill_qr' ? record.sale.link.phone : null;
  const text = (url: string) => intl.formatMessage({ id: 'pos.receipt.shareText' }, { shop: shopName, url });
  return (
    <Sheet open={open && record !== null} onClose={onClose} title={intl.formatMessage({ id: 'pos.receipt.title' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
      {record && (
        <div className={styles.sheetBody}>
          <ReceiptView shopName={shopName} sale={record.sale} />
          {token ? (
            <div className={styles.actions}>
              <a className="tz-btn tz-btn-soft" href={whatsappShareUrl(text(receiptUrl(token)), phone)} target="_blank" rel="noreferrer noopener">
                <MessageCircle className="tz-ico" aria-hidden="true" />
                {intl.formatMessage({ id: 'pos.receipt.whatsapp' })}
              </a>
              <a className="tz-btn tz-btn-soft" href={smsShareUrl(text(receiptUrl(token)), phone)}>
                <MessageSquare className="tz-ico" aria-hidden="true" />
                {intl.formatMessage({ id: 'pos.receipt.sms' })}
              </a>
            </div>
          ) : (
            <p className="tz-sub" role="status">
              {intl.formatMessage({ id: 'pos.receipt.linkPending' })}
            </p>
          )}
          <Button variant="primary" icon={Printer} onClick={() => window.print()}>
            {intl.formatMessage({ id: 'pos.receipt.print' })}
          </Button>
        </div>
      )}
    </Sheet>
  );
}
