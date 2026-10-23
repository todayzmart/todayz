import { describe, expect, it } from 'vitest';
import { CHART, axisMaxRupees, chartModel } from './chart';
import type { HourPoint } from './types';

const points: HourPoint[] = [
  { hour: 8, todayPaise: 1200_00, prevPaise: 1000_00 },
  { hour: 9, todayPaise: 2600_00, prevPaise: 2200_00 },
  { hour: 10, todayPaise: 6400_00, prevPaise: 5900_00 },
  { hour: 11, todayPaise: null, prevPaise: 6800_00 },
];

describe('sales-by-hour chart model', () => {
  it('rounds the axis up to the next ₹2,000 (at least ₹2,000)', () => {
    expect(axisMaxRupees(points)).toBe(8000);
    expect(axisMaxRupees([{ hour: 8, todayPaise: 0, prevPaise: 0 }])).toBe(2000);
  });

  it('marks only the hour in progress as current and skips future hours', () => {
    const m = chartModel(points, 10);
    expect(m.bars.map((b) => b.hour)).toEqual([8, 9, 10]);
    expect(m.bars.filter((b) => b.current).map((b) => b.hour)).toEqual([10]);
    expect(m.current?.paise).toBe(6400_00);
    expect(m.currentPrevPaise).toBe(5900_00);
  });

  it('keeps bars inside the plot and draws last week across every hour', () => {
    const m = chartModel(points, 10);
    for (const b of m.bars) {
      expect(b.y).toBeGreaterThanOrEqual(CHART.top);
      expect(b.y + b.height).toBeCloseTo(CHART.bottom, 0);
    }
    expect(m.prevPath.split(' ')).toHaveLength(points.length);
    expect(m.ticks.map((t) => t.rupees)).toEqual([0, 2000, 4000, 6000, 8000]);
  });

  it('has no current bar before the shop opens', () => {
    expect(chartModel(points, 7).current).toBeUndefined();
  });
});
