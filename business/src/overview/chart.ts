import type { HourPoint } from './types';

/** Geometry of the sales-by-hour chart, in the design card's 600 × 210 viewBox. Pure, so it is unit tested. */
export const CHART = { width: 600, height: 210, left: 36, right: 592, top: 12, bottom: 182, bar: 22 } as const;

export interface ChartBar {
  hour: number;
  x: number;
  y: number;
  height: number;
  /** The hour in progress: solid brand. Earlier hours are grey. */
  current: boolean;
  paise: number;
}

export interface ChartModel {
  /** Y-axis ticks in rupees (0, 2000, …) with their y. */
  ticks: { rupees: number; y: number }[];
  /** X-axis labels every two hours. */
  hours: { hour: number; x: number }[];
  bars: ChartBar[];
  /** SVG path of last week's line. */
  prevPath: string;
  current: ChartBar | undefined;
  /** Last week's amount in the current hour. */
  currentPrevPaise: number | undefined;
}

/** A round axis top: the next multiple of 2,000 rupees above the largest value (at least 2,000). */
export function axisMaxRupees(points: readonly HourPoint[]): number {
  const max = Math.max(0, ...points.map((p) => Math.max(p.todayPaise ?? 0, p.prevPaise))) / 100;
  return Math.max(2000, Math.ceil(max / 2000) * 2000);
}

const r1 = (n: number) => Math.round(n * 10) / 10;

export function chartModel(points: readonly HourPoint[], currentHour: number): ChartModel {
  const maxRupees = axisMaxRupees(points);
  const plotH = CHART.bottom - CHART.top;
  const yOf = (paise: number) => r1(CHART.bottom - (paise / 100 / maxRupees) * plotH);
  const step = points.length > 1 ? (CHART.right - CHART.left) / (points.length - 1) : 0;
  const xOf = (i: number) => r1(CHART.left + i * step);

  const ticks = [];
  for (let rupees = 0; rupees <= maxRupees; rupees += 2000) ticks.push({ rupees, y: yOf(rupees * 100) });

  const hours = points.flatMap((p, i) => (p.hour % 2 === 0 ? [{ hour: p.hour, x: xOf(i) }] : []));

  const bars: ChartBar[] = points.flatMap((p, i) => {
    if (p.todayPaise === null) return [];
    const y = yOf(p.todayPaise);
    return [{ hour: p.hour, x: r1(xOf(i) - CHART.bar / 2), y, height: r1(CHART.bottom - y), current: p.hour === currentHour, paise: p.todayPaise }];
  });

  const prevPath = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${xOf(i)},${yOf(p.prevPaise)}`).join(' ');
  const current = bars.find((b) => b.current);
  const currentPrevPaise = points.find((p) => p.hour === currentHour)?.prevPaise;
  return { ticks, hours, bars, prevPath, current, currentPrevPaise };
}
