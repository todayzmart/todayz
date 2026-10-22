import { createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router';
import { describe, expect, it } from 'vitest';

/**
 * The contract the folder CLAUDE.md gives every WP: adding a static route file for a section
 * (src/routes/orders/index.tsx, src/routes/orders.tsx, …) takes that URL away from the `/$section`
 * placeholder, with no change to nav.ts. This pins the router behaviour that makes it true.
 */
function routerWith(path: string) {
  const root = createRootRoute();
  const section = createRoute({ getParentRoute: () => root, path: '$section' });
  const orders = createRoute({ getParentRoute: () => root, path: 'orders' });
  const ordersIndex = createRoute({ getParentRoute: () => orders, path: '/' });
  const orderDetail = createRoute({ getParentRoute: () => orders, path: '$orderId' });
  return createRouter({
    // Children listed after the param route, so the order of declaration cannot be what wins.
    routeTree: root.addChildren([section, orders.addChildren([ordersIndex, orderDetail])]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
}

async function leafRouteId(path: string) {
  const router = routerWith(path);
  await router.load();
  return router.state.matches.at(-1)?.routeId;
}

describe('route precedence', () => {
  it('a static section route outranks the /$section placeholder', async () => {
    expect(await leafRouteId('/orders')).toBe('/orders/');
    expect(await leafRouteId('/orders/01J9Z')).toBe('/orders/$orderId');
  });

  it('sections without a route still fall through to the placeholder', async () => {
    expect(await leafRouteId('/stock')).toBe('/$section');
  });
});
