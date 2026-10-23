import { act, renderHook } from '@testing-library/react';
import type { StoreOrderSummary } from '@todayz/api-client/gen/model/storeOrderSummary';
import { beforeEach, describe, expect, it } from 'vitest';
import { acknowledgeOrder, resetAlerts, useAlertingOrders } from './alerts';

const order = (id: string, seen: boolean) => ({ id, seen }) as StoreOrderSummary;

beforeEach(() => resetAlerts());

describe('order alerts', () => {
  it('does not alert for orders already seen when the page first loads', () => {
    const { result } = renderHook(() => useAlertingOrders('s1', [order('a', true), order('b', false)]));
    expect(result.current.map((o) => o.id)).toEqual(['b']);
  });

  it('alerts for a later order until it is acknowledged, even if the server marks it seen', () => {
    const { result, rerender } = renderHook(({ items }) => useAlertingOrders('s1', items), {
      initialProps: { items: [order('a', true)] },
    });
    expect(result.current).toEqual([]);
    rerender({ items: [order('a', true), order('c', false)] });
    expect(result.current.map((o) => o.id)).toEqual(['c']);
    rerender({ items: [order('a', true), order('c', true)] });
    expect(result.current.map((o) => o.id)).toEqual(['c']);
    act(() => acknowledgeOrder('s1', 'c'));
    expect(result.current).toEqual([]);
  });

  it('keeps stores apart', () => {
    const { result } = renderHook(() => useAlertingOrders('s2', [order('a', false)]));
    act(() => acknowledgeOrder('s1', 'a'));
    expect(result.current.map((o) => o.id)).toEqual(['a']);
  });
});
