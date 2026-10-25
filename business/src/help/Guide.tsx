import { Link } from '@tanstack/react-router';
import { Phone } from 'lucide-react';
import { useIntl } from 'react-intl';
import { supportPhone } from './contact';
import styles from './help.module.css';

/** "How TodayZ works for your shop", in the order a new shop meets it (01 §4–§9). */
const STEPS = ['stock', 'orders', 'pickup', 'counter', 'credits', 'payouts'] as const;

/** The questions shops ask first. The copy names no configurable number (those live in settings). */
const FAQS = ['timer', 'code', 'credits', 'payout', 'visible', 'staff', 'close'] as const;

/** B-15 guide, FAQs and how to reach support. Every role sees it; only tickets are role-gated. */
export function Guide({ tickets }: { tickets: boolean }) {
  const intl = useIntl();
  const phone = supportPhone();
  return (
    <div className={styles.guide}>
      <section className="tz-card" aria-labelledby="help-how">
        <h2 id="help-how" className="tz-card-title">
          {intl.formatMessage({ id: 'help.how.title' })}
        </h2>
        <ol className={`tz-steps ${styles.steps}`}>
          {STEPS.map((s, i) => (
            <li key={s} data-done="">
              <span className="tz-dot" aria-hidden="true">
                {i + 1}
              </span>
              <div>
                <b>{intl.formatMessage({ id: `help.how.${s}.title` })}</b>
                <small>{intl.formatMessage({ id: `help.how.${s}.body` })}</small>
              </div>
            </li>
          ))}
        </ol>
      </section>

      <section className="tz-card" aria-labelledby="help-faq">
        <h2 id="help-faq" className="tz-card-title">
          {intl.formatMessage({ id: 'help.faq.title' })}
        </h2>
        <div className={styles.faqs}>
          {FAQS.map((f) => (
            <details key={f} className={styles.faq}>
              <summary>{intl.formatMessage({ id: `help.faq.${f}.q` })}</summary>
              <p>{intl.formatMessage({ id: `help.faq.${f}.a` })}</p>
            </details>
          ))}
        </div>
      </section>

      <section className="tz-card" aria-labelledby="help-contact">
        <h2 id="help-contact" className="tz-card-title">
          {intl.formatMessage({ id: 'help.contact.title' })}
        </h2>
        <p className={styles.note}>
          {intl.formatMessage({ id: tickets ? 'help.contact.body' : 'help.contact.bodyCashier' })}
        </p>
        <div className={styles.actions}>
          {phone && (
            <a href={`tel:${phone}`} className={`tz-btn ${styles.linkBtn}`}>
              <Phone className="tz-ico tz-ico-sm" aria-hidden="true" />
              {intl.formatMessage({ id: 'help.contact.call' }, { phone })}
            </a>
          )}
          {tickets && (
            <Link to="/help/new" className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
              {intl.formatMessage({ id: 'help.tickets.new' })}
            </Link>
          )}
        </div>
      </section>
    </div>
  );
}
