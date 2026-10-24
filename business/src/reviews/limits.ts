/**
 * Review rules this screen needs. The contract fixes a reply at 1 to 1000 characters and a report
 * reason at 1 to 500; the server is the real gate (422 shows as a message).
 */

/** The reply length in `ReplyRequest` (backend/api/schemas/reviews.yaml). */
export const REPLY_MAX = 1000;

/**
 * TODO(lead): the contract takes the report reason as free text (up to 500 characters). These are the
 * reasons the screen offers; the chosen code, then the optional note, is sent as that text.
 */
export const REPORT_REASONS = ['abusive', 'spam', 'fake', 'not_about_shop', 'other'] as const;
export type ReportReason = (typeof REPORT_REASONS)[number];

/** The reason text is the code plus the note, at most 500 characters in the contract. */
export const REPORT_TEXT_MAX = 500;
export const REPORT_NOTE_MAX = 300;

/** Reviews with this many stars or fewer count as "low" in the filter. */
export const LOW_STARS_MAX = 2;

export const PAGE_SIZE = 20;

/** Plain text, trimmed; null when empty. */
export function cleanReply(text: string): string | null {
  const t = text.trim();
  return t.length === 0 ? null : t;
}
