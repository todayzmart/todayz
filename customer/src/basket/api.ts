// Saved baskets (04 me/baskets): query keys and the If-Match helper. Every write to a saved basket sends the
// version it read; a 412 means another device changed it, so the screen reloads it (BSK-13).
import { ApiError } from '@todayz/api-client';
import { createBasket, getMyBasket, listMyBaskets, updateBasket } from '@todayz/api-client/gen/baskets/baskets';
import type { Basket } from '@todayz/api-client/gen/model/basket';
import type { BasketPatch } from '@todayz/api-client/gen/model/basketPatch';
import type { QueryClient } from '@tanstack/react-query';
import { toInput, type EditLine } from './model';

export const basketsKey = ['me', 'baskets'] as const;
export const basketKey = (id: string) => ['me', 'baskets', id] as const;

export const loadBaskets = () => listMyBaskets();
export const loadBasket = (id: string) => getMyBasket(id);

export function patchBasket(b: Pick<Basket, 'basketId' | 'version'>, patch: BasketPatch): Promise<Basket> {
  return updateBasket(b.basketId, patch, { ifMatch: `"v${b.version}"` });
}

export function saveNew(name: string, lines: readonly EditLine[]): Promise<Basket> {
  return createBasket({ name, lines: toInput(lines) });
}

/** After a write: the basket's own query gets the answer, the list is read again. */
export function stored(qc: QueryClient, b: Basket): void {
  qc.setQueryData(basketKey(b.basketId), b);
  void qc.invalidateQueries({ queryKey: basketsKey, exact: true });
}

export function isConflict(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 412 || err.status === 428);
}

export function isLimit(err: unknown): boolean {
  return err instanceof ApiError && err.code === 'basket_limit_reached';
}

export function isTooMany(err: unknown): boolean {
  return err instanceof ApiError && err.code === 'validation_failed';
}
