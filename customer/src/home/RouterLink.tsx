import { useRouter } from '@tanstack/react-router';
import type { LinkProps } from '@todayz/ui';

/** Client-side navigation for the `href` links of @todayz/ui; modified clicks keep the browser's behaviour. */
export function RouterLink({ href, onClick, children, ...rest }: LinkProps) {
  const router = useRouter();
  return (
    <a
      href={href}
      {...rest}
      onClick={(e) => {
        onClick?.(e);
        if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        if (rest.target && rest.target !== '_self') return;
        e.preventDefault();
        router.history.push(href);
      }}
    >
      {children}
    </a>
  );
}
