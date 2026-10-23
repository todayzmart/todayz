// Shared gate for everything that leaves the device: RUM, product events. Local dev, tests and builds
// without an endpoint make no network calls at all.

export type SendEnv = 'local' | 'test' | 'staging' | 'production';

/** Pure: may anything be sent to this endpoint in this environment? */
export function endpointAllowed(env: SendEnv, endpoint: string | undefined, forceLocal = false): boolean {
  if (!endpoint) return false;
  if (env === 'test') return false;
  if (env === 'local' && !forceLocal) return false;
  try {
    const { protocol } = new URL(endpoint);
    return protocol === 'https:' || protocol === 'http:';
  } catch {
    return false;
  }
}

/** Sends a JSON body with sendBeacon, falling back to a keepalive fetch. Never throws. */
export function sendJson(endpoint: string, body: string): void {
  try {
    const blob = new Blob([body], { type: 'application/json' });
    if (navigator.sendBeacon?.(endpoint, blob)) return;
    void fetch(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
      credentials: 'omit',
    }).catch(() => undefined);
  } catch {
    // Telemetry must never break the app.
  }
}
