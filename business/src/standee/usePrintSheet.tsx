import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { StandeeSheet, type SheetContent } from './StandeeSheet';
import styles from './sheet.module.css';

/**
 * Prints one sheet through the browser's own dialog ("Save as PDF" is in it on every platform). The
 * sheet goes into a portal on `body`; the print stylesheet hides everything else while it is there.
 */
export function usePrintSheet() {
  const [sheet, setSheet] = useState<SheetContent | null>(null);

  useEffect(() => {
    if (!sheet) return;
    const done = () => setSheet(null);
    window.addEventListener('afterprint', done);
    // One frame so the portal is painted before the dialog snapshots the page.
    const id = window.requestAnimationFrame(() => window.print());
    return () => {
      window.cancelAnimationFrame(id);
      window.removeEventListener('afterprint', done);
    };
  }, [sheet]);

  const portal = sheet
    ? createPortal(
        <div className={styles.printRoot} data-standee-print="">
          <StandeeSheet {...sheet} />
        </div>,
        document.body,
      )
    : null;
  return { print: setSheet, portal };
}
