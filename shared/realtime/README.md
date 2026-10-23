# @todayz/realtime

Client for the three server-sent event streams (`/v1/me/stream`, `/v1/biz/businesses/{bid}/stores/{sid}/stream`, `/v1/admin/stream`). It uses a streaming `fetch` with `Authorization: Bearer` (EventSource cannot send headers), resumes with `Last-Event-ID`, treats the server's `reconnect` frame as a normal handover, backs off on failures, and after three in a row polls every 15 s while the page is visible until a `ready` frame arrives. A `reset` frame means the cursor was too old, so every key in `resetKeys` is invalidated. No dependencies; the query client is passed in structurally.

```ts
import { useRealtimeInvalidation } from '@todayz/realtime';

useRealtimeInvalidation(
  {
    url: `${apiUrl}/v1/biz/businesses/${bid}/stores/${storeId}/stream`,
    ownerKey: userId,
    getToken: () => session.getToken(),
    refresh: () => session.refresh(),
    queryClient,
    resetKeys: [['orders', storeId]],
  },
  {
    'order.created': () => [['orders', storeId]],
    'order.updated': (e) => [['orders', storeId], ['order', e.id]],
  },
);
```

Tests: `mk/47-realtime.mk`.
