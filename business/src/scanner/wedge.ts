// Keyboard-wedge scanners type the code like a very fast keyboard and finish with Enter. A person
// typing the same characters is far slower, so the gap between keys tells them apart (POS-15).
import { useEffect, useRef } from 'react';

export interface WedgeOptions {
  /** Longest gap between two keys of one scan. Scanners send a key every few ms; people take 100 ms or more. */
  maxGapMs?: number;
  /** Shortest code that counts as a scan. */
  minLength?: number;
}

export interface KeyLike {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
}

export const WEDGE_MAX_GAP_MS = 60;
export const WEDGE_MIN_LENGTH = 4;

export function createWedgeDetector(opts: WedgeOptions = {}) {
  const maxGap = opts.maxGapMs ?? WEDGE_MAX_GAP_MS;
  const minLength = opts.minLength ?? WEDGE_MIN_LENGTH;
  let buf = '';
  let last = 0;
  return {
    /** Feed every key press with its time. Returns the scanned code when Enter ends a fast run. */
    key(e: KeyLike, t: number): string | null {
      if (e.ctrlKey || e.metaKey || e.altKey) return null;
      if (e.key === 'Enter' || e.key === 'Tab') {
        const code = buf;
        const fast = t - last <= maxGap;
        buf = '';
        return fast && code.length >= minLength ? code : null;
      }
      if (e.key.length !== 1) return null;
      if (t - last > maxGap) buf = '';
      buf += e.key;
      last = t;
      return null;
    },
    reset() {
      buf = '';
    },
  };
}

function isEditable(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  if (el.isContentEditable) return true;
  const tag = el.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
}

/**
 * Listens for scanner input anywhere on the page while no text field has focus (inside a field the
 * Enter key of that field handles the code). `onScan` gets the digits and letters only.
 */
export function useWedgeScanner(onScan: (code: string) => void, enabled = true): void {
  const handler = useRef(onScan);
  useEffect(() => {
    handler.current = onScan;
  }, [onScan]);
  useEffect(() => {
    if (!enabled) return;
    const det = createWedgeDetector();
    const onKey = (e: KeyboardEvent) => {
      if (isEditable(document.activeElement)) {
        det.reset();
        return;
      }
      const code = det.key(e, e.timeStamp || performance.now());
      if (code) {
        e.preventDefault();
        handler.current(code);
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [enabled]);
}

/** Barcodes are digits (EAN, UPC) or short alphanumeric codes; a typed word is not one. */
export function looksLikeBarcode(text: string): boolean {
  return /^[0-9]{6,14}$/.test(text.trim());
}
