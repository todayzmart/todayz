import { Link } from '@tanstack/react-router';
import { Mic } from 'lucide-react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useVoiceAvailability } from './queries';
import styles from './voice.module.css';

/** The way into voice update from Stock; nothing renders while voice is off for the shop. */
export function VoiceLink({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const avail = useVoiceAvailability(store);
  if (!avail.data) return null;
  return (
    <Link to="/voice" className={`tz-btn ${styles.link}`}>
      <Mic aria-hidden="true" size={18} />
      {intl.formatMessage({ id: 'voice.link' })}
    </Link>
  );
}
