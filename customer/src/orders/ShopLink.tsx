import { useRouter } from '@tanstack/react-router';
import type { ReactNode } from 'react';

/**
 * A link to a route that another work package owns (/s/{slug}, /search): it is an ordinary anchor
 * that the router handles, so this folder does not depend on those routes existing yet.
 */
export function PathLink({ to, className, children }: { to: string; className?: string; children: ReactNode }) {
  const router = useRouter();
  return (
    <a
      href={to}
      className={className}
      onClick={(e) => {
        if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        router.history.push(to);
      }}
    >
      {children}
    </a>
  );
}
