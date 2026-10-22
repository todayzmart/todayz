import type { LucideIcon } from 'lucide-react';

/** Lucide icon with the design system's tz-ico size and 1.5 stroke; decorative by default. */
export function Icon({ icon: Glyph, className }: { icon: LucideIcon; className?: string }) {
  return <Glyph className={className ? `tz-ico ${className}` : 'tz-ico'} strokeWidth={1.5} aria-hidden="true" focusable="false" />;
}
