// MSW for the apps' tests, stories and `pnpm dev` without a backend (08 §3.5).
// `handlers` answers every operation with its contract example; a test overrides
// one with `server.use(http.post('*/v1/me/orders', () => problemResponse('insufficient_stock', 409)))`.
import { HttpResponse } from 'msw';

import type { Problem } from '../http';

export { examples, handlers } from './handlers.gen';

/** A problem+json answer shaped like the API's (04 §1.4) for a registered code. */
export function problemResponse(code: string, status: number, extra: Partial<Problem> = {}): Response {
  const body: Problem = {
    type: `https://todayz.in/errors/${code}`,
    title: code.replaceAll('_', ' '),
    status,
    code,
    ...extra,
  };
  return HttpResponse.json(body, { status, headers: { 'Content-Type': 'application/problem+json' } });
}
