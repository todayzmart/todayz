import type { ReactNode } from 'react';

/** Text for screen readers only (tz-ui-sr-only in styles.css). */
export function VisuallyHidden({ children }: { children: ReactNode }) {
  return <span className="tz-ui-sr-only">{children}</span>;
}
