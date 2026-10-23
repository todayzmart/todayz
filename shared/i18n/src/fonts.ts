// Font loading per script. The @font-face rules (Anek, Figtree, Noto Sans Devanagari/Kannada/Telugu/
// Tamil, self-hosted, one unicode-range subset per script) come from @todayz/tokens fonts.css, so
// the browser fetches a script's file only when text in that script is on the page. This module
// names the families per language and lets an app load the right ones before first paint.
import { SCRIPT_OF, baseLocale, type MessageLocale, type Script } from './locales';

/** [display (Anek), sans (Noto Sans or Figtree)] family for each script. */
export const FONT_FAMILIES: Record<Script, { display: string; sans: string }> = {
  latin: { display: 'Anek Latin', sans: 'Figtree' },
  devanagari: { display: 'Anek Devanagari', sans: 'Noto Sans Devanagari' },
  kannada: { display: 'Anek Kannada', sans: 'Noto Sans Kannada' },
  telugu: { display: 'Anek Telugu', sans: 'Noto Sans Telugu' },
  tamil: { display: 'Anek Tamil', sans: 'Noto Sans Tamil' },
};

/** A few real characters of each script, to make the browser fetch the right subset. */
export const SCRIPT_SAMPLE: Record<Script, string> = {
  latin: 'Aa0₹',
  devanagari: 'अआक',
  kannada: 'ಅಆಕ',
  telugu: 'అఆక',
  tamil: 'அஆக',
};

export function scriptOf(locale: MessageLocale): Script {
  return SCRIPT_OF[baseLocale(locale)];
}

/** Families the current language needs first: its own script, then Latin (digits, ₹, product names). */
export function fontFamiliesFor(locale: MessageLocale): string[] {
  const s = scriptOf(locale);
  const own = FONT_FAMILIES[s];
  return s === 'latin' ? [own.display, own.sans] : [own.display, own.sans, FONT_FAMILIES.latin.display, FONT_FAMILIES.latin.sans];
}

/** CSS `font:` shorthands to hand to `document.fonts.load`, 400 for text and 700 for display. */
export function fontLoadRequests(locale: MessageLocale): { font: string; text: string }[] {
  const s = scriptOf(locale);
  const out = [{ font: `400 16px "${FONT_FAMILIES[s].sans}"`, text: SCRIPT_SAMPLE[s] }, { font: `700 16px "${FONT_FAMILIES[s].display}"`, text: SCRIPT_SAMPLE[s] }];
  if (s !== 'latin') out.push({ font: `400 16px "${FONT_FAMILIES.latin.sans}"`, text: SCRIPT_SAMPLE.latin });
  return out;
}

/** The subset of FontFaceSet that loadFonts needs. */
export interface FontLoader {
  load(font: string, text?: string): Promise<unknown>;
}

/**
 * Starts loading the fonts for a language and resolves when they are ready or failed; it never
 * rejects, because text then simply renders in the fallback stack (system-ui).
 */
export async function loadFonts(locale: MessageLocale, loader: FontLoader | undefined = globalThis.document?.fonts): Promise<void> {
  if (!loader) return;
  await Promise.allSettled(fontLoadRequests(locale).map((r) => loader.load(r.font, r.text)));
}
