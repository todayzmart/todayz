import { formatClock, formatMoney, istParts } from '@todayz/i18n';
import { useIntl } from 'react-intl';
import { CHART, chartModel } from './chart';
import { axisHour, hourLabel, weekday } from './format';
import { RouterLink } from './RouterLink';
import styles from './overview.module.css';
import type { OverviewData } from './types';

/**
 * Sales by hour (plain SVG, tokens only): today's bars, the hour in progress in brand and the
 * earlier hours grey, the same weekday last week as a dotted line, and an inverse tooltip on the
 * current hour.
 */
export function SalesByHour({ data }: { data: OverviewData }) {
  const intl = useIntl();
  const currentHour = istParts(data.now).hour;
  const m = chartModel(data.salesByHour, currentHour);
  const day = weekday(intl, data.now);
  const cur = m.current;
  const nowText = cur ? formatMoney(cur.paise, 'dashboard') : '';
  const prevText = m.currentPrevPaise === undefined ? '' : formatMoney(m.currentPrevPaise, 'dashboard');
  const hourText = hourLabel(intl, currentHour);
  // The tooltip box sits left of the bar centre, as on the design card; past the middle it hangs
  // from its right edge instead, so a late hour never pushes it off a phone screen.
  const centre = cur ? ((cur.x + CHART.bar / 2) / CHART.width) * 100 : 0;
  const tipStyle = centre > 50 ? { right: `${Math.max(0, 100 - centre - 14).toFixed(1)}%` } : { left: `${Math.max(0, centre - 14).toFixed(1)}%` };
  return (
    <section className={`tz-card ${styles.chart}`} aria-labelledby="ov-chart-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="ov-chart-title">
          {intl.formatMessage({ id: 'overview.chart.title' })}
        </h2>
        <div className="tz-legend" aria-hidden="true">
          <span>
            <i className={styles.legendBar} />
            {intl.formatMessage({ id: 'overview.chart.today' })}
          </span>
          <span>
            <i className="prev" />
            {intl.formatMessage({ id: 'overview.chart.prev' }, { weekday: day })}
          </span>
        </div>
      </div>
      <div className={styles.chartBox}>
        <svg
          className="tz-chart"
          viewBox={`0 0 ${CHART.width} ${CHART.height}`}
          role="img"
          aria-label={intl.formatMessage(
            { id: 'overview.chart.a11y' },
            { weekday: day, hour: hourText, now: nowText, prev: prevText },
          )}
        >
          {m.ticks.map((t) => (
            <g key={t.rupees}>
              <line className="grid" x1={CHART.left} x2={CHART.width} y1={t.y} y2={t.y} />
              <text className="axis" x={28} y={t.y + 4} textAnchor="end">
                {intl.formatMessage({ id: 'overview.chart.axisK' }, { k: t.rupees / 1000 })}
              </text>
            </g>
          ))}
          {m.hours.map((h) => (
            <text key={h.hour} className="axis" x={h.x} y={202} textAnchor="middle">
              {axisHour(intl, h.hour)}
            </text>
          ))}
          {m.bars.map((b) => (
            <rect
              key={b.hour}
              className={b.current ? 'bar-hl' : 'bar-pale'}
              data-current={b.current || undefined}
              x={b.x}
              y={b.y}
              width={CHART.bar}
              height={b.height}
              rx={6}
            />
          ))}
          <path className="line-prev" d={m.prevPath} />
        </svg>
        {cur && (
          <div className={`tz-tip ${styles.tip}`} style={tipStyle} aria-hidden="true">
            {intl.formatMessage({ id: 'overview.chart.tipHour' }, { hour: hourText })}
            <b>{nowText}</b>
            {intl.formatMessage({ id: 'overview.chart.tipPrev' }, { weekday: weekday(intl, data.now, 'short'), amount: prevText })}
          </div>
        )}
      </div>
      <div className="tz-card-foot">
        <RouterLink href="/analytics">{intl.formatMessage({ id: 'overview.chart.report' })}</RouterLink>
        <span>{intl.formatMessage({ id: 'overview.chart.updated' }, { time: formatClock(data.syncedAt) })}</span>
      </div>
    </section>
  );
}
