import { useIntl } from 'react-intl';
import { QrCode } from '../pos/QrCode';
import styles from './sheet.module.css';

export interface SheetContent {
  shop: string;
  /** The URL the QR carries: the store link (`?k=s`) or one campaign's (`?s=<id>`). */
  link: string;
  code: string;
  /** A campaign standee prints its name small at the foot, so the shop can tell them apart. */
  campaign?: string | undefined;
}

/**
 * The A5 standee, drawn in the page (D-278): shop name, QR and short code. The same markup is the
 * on-screen preview and the printed sheet; sizes are in container units, so it scales to either.
 */
export function StandeeSheet({ shop, link, code, campaign }: SheetContent) {
  const intl = useIntl();
  return (
    <div className={styles.sheet}>
      <div className={styles.inner}>
        <p className={styles.brand}>
          Today<b>Z</b>
        </p>
        <p className={styles.lead}>{intl.formatMessage({ id: 'standee.sheet.lead' })}</p>
        <p className={styles.shop}>{shop}</p>
        <QrCode className={styles.qr} text={link} label={intl.formatMessage({ id: 'standee.sheet.qr' }, { shop })} />
        <p className={styles.hint}>{intl.formatMessage({ id: 'standee.sheet.hint' })}</p>
        <p className={styles.code}>
          {intl.formatMessage({ id: 'standee.sheet.code' })} <b>{code}</b>
        </p>
        {campaign && <p className={styles.campaign}>{campaign}</p>}
      </div>
    </div>
  );
}
