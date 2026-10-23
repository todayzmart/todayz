import { Segmented, StatusPill } from '@todayz/ui';
import { FormattedMessage, useIntl } from 'react-intl';
import { dayPart, longDate, weekday } from './format';
import styles from './overview.module.css';
import type { OverviewData, Period } from './types';

interface GreetBandProps {
  data: OverviewData;
  period: Period;
  onPeriod: (p: Period) => void;
}

const bold = (chunks: React.ReactNode[]) => <b>{chunks}</b>;

/** Mint greeting band: greeting, date, shop, waiting orders, the period switch and the comparison. */
export function GreetBand({ data, period, onPeriod }: GreetBandProps) {
  const intl = useIntl();
  const name = { name: data.ownerName };
  const part = dayPart(data.now);
  const title =
    part === 'morning'
      ? intl.formatMessage({ id: 'overview.greet.morning' }, name)
      : part === 'afternoon'
        ? intl.formatMessage({ id: 'overview.greet.afternoon' }, name)
        : intl.formatMessage({ id: 'overview.greet.evening' }, name);
  return (
    <div className={`tz-greet ${styles.greet}`}>
      <div className={styles.greetText}>
        <div className={styles.greetTitle}>
          <h1 className="tz-page-title">{title}</h1>
          {data.isSample && <StatusPill>{intl.formatMessage({ id: 'overview.sample' })}</StatusPill>}
        </div>
        <p className="tz-page-sub">
          {intl.formatMessage(
            { id: 'overview.greet.sub' },
            { date: longDate(intl, data.now), store: data.storeName, waiting: data.kpis.waitingOrders },
          )}
        </p>
      </div>
      <div className={styles.greetControls}>
        <Segmented<Period>
          label={intl.formatMessage({ id: 'overview.period.label' })}
          value={period}
          onChange={onPeriod}
          options={[
            { value: 'today', label: intl.formatMessage({ id: 'overview.period.today' }) },
            { value: '7d', label: intl.formatMessage({ id: 'overview.period.7d' }) },
            { value: '30d', label: intl.formatMessage({ id: 'overview.period.30d' }) },
          ]}
        />
        <span className="tz-filter" data-set="">
          {period === 'today' ? (
            <FormattedMessage id="overview.compare.today" values={{ weekday: weekday(intl, data.now), b: bold }} />
          ) : period === '7d' ? (
            <FormattedMessage id="overview.compare.7d" values={{ b: bold }} />
          ) : (
            <FormattedMessage id="overview.compare.30d" values={{ b: bold }} />
          )}
        </span>
      </div>
    </div>
  );
}
