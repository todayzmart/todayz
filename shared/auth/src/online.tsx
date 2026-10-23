import { Banner } from '@todayz/ui';
import { useSyncExternalStore } from 'react';

function subscribe(cb: () => void): () => void {
  window.addEventListener('online', cb);
  window.addEventListener('offline', cb);
  return () => {
    window.removeEventListener('online', cb);
    window.removeEventListener('offline', cb);
  };
}

/** True while the browser reports a network. */
export function useOnline(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
}

export interface OfflineBannerProps {
  title: string;
  message: string;
  className?: string | undefined;
}

/** The "you're offline" banner (G9); nothing while online. Strings come from the app's catalogue. */
export function OfflineBanner({ title, message, className }: OfflineBannerProps) {
  const online = useOnline();
  if (online) return null;
  return (
    <Banner tone="warning" live="status" title={title} className={className}>
      {message}
    </Banner>
  );
}
