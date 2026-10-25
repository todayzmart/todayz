import type { BizAttachmentContentType } from '@todayz/api-client/gen/model/bizAttachmentContentType';
import type { BizTicketCategory } from '@todayz/api-client/gen/model/bizTicketCategory';

/**
 * Ticket rules this screen needs, all fixed by the contract (backend/api/schemas/biz-support.yaml),
 * not settings: the server is the real gate and a 422 shows as a message.
 */

/** `BizTicketCreate.subject`. */
export const SUBJECT_MIN = 3;
export const SUBJECT_MAX = 120;

/** `BizTicketCreate.body` and `BizTicketMessageCreate.body`. */
export const BODY_MAX = 4000;

/** `BizAttachmentCreate`: these types, at most 10 MB each, at most 5 per message. */
export const ACCEPT: readonly BizAttachmentContentType[] = ['image/jpeg', 'image/png', 'image/webp', 'application/pdf'];
export const MAX_BYTES = 10 * 1024 * 1024;
export const MAX_FILES = 5;

/** The order the category picker lists them in (`BizTicketCategory`). */
export const CATEGORIES: readonly BizTicketCategory[] = ['order', 'credits', 'payouts', 'catalogue', 'account', 'verification', 'app', 'other'];

export const PAGE_SIZE = 20;

/** Recent orders offered in the "Which order?" picker. */
export const RECENT_ORDERS = 30;
