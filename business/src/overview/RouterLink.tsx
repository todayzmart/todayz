import type { LinkProps } from '@todayz/ui';
import { useRouter } from '@tanstack/react-router';
import type { MouseEvent } from 'react';

/**
 * The kit's LinkComponent for this app: a real `<a href>` (open in new tab still works) that
 * navigates client-side on a plain click. Hrefs carry the insight deep-link query (06-screens).
 */
export function RouterLink({ href, onClick, children, ...rest }: LinkProps) {
  const router = useRouter();
  const go = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    void router.navigate({ href });
  };
  return (
    <a href={href} onClick={go} {...rest}>
      {children}
    </a>
  );
}
