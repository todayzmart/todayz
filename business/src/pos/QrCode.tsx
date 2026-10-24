import { useMemo } from 'react';
import { encodeQr, qrPath } from './qr';

export interface QrCodeProps {
  text: string;
  /** Accessible name, e.g. "Bill QR". */
  label: string;
  size?: number;
  className?: string | undefined;
}

/** Draws a QR code as one SVG path. Light theme only, so black on white with the 4-module quiet zone. */
export function QrCode({ text, label, size = 224, className }: QrCodeProps) {
  const path = useMemo(() => qrPath(encodeQr(text)), [text]);
  return (
    <svg className={className} role="img" aria-label={label} width={size} height={size} viewBox={path.viewBox} shapeRendering="crispEdges">
      <rect width="100%" height="100%" fill="var(--surface)" />
      <path d={path.d} fill="var(--ink)" />
    </svg>
  );
}
