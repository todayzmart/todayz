import type { AnchorHTMLAttributes, ComponentType, ReactNode } from 'react';

/**
 * Props every link renderer receives. Apps pass their router's link (for example a TanStack
 * Router `Link` wrapped to take `href`) so navigation stays client-side; the default is `<a>`.
 */
export type LinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & { href: string; children?: ReactNode };
export type LinkComponent = ComponentType<LinkProps>;
