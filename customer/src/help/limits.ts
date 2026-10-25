import type { MyTicketAttachmentContentType } from '@todayz/api-client/gen/model/myTicketAttachmentContentType';
import type { MyTicketCategory } from '@todayz/api-client/gen/model/myTicketCategory';

/**
 * Ticket rules this screen needs, all fixed by the contract (backend/api/schemas/support.yaml), not
 * settings: the server is the real gate and a 422 shows as a message.
 */

/** `MyTicketCreate.subject`. */
export const SUBJECT_MIN = 3;
export const SUBJECT_MAX = 120;

/** `MyTicketCreate.body` and `MyTicketMessageCreate.body`. */
export const BODY_MAX = 4000;

/** `MyTicketAttachmentCreate`: these types, at most 10 MB each, at most 5 per message. */
export const ACCEPT: readonly MyTicketAttachmentContentType[] = ['image/jpeg', 'image/png', 'image/webp', 'application/pdf'];
export const MAX_BYTES = 10 * 1024 * 1024;
export const MAX_FILES = 5;

/** The order the topic picker lists them in (`MyTicketCategory`). */
export const CATEGORIES: readonly MyTicketCategory[] = ['order', 'credits', 'account', 'app', 'other'];

export const PAGE_SIZE = 20;

/** Recent orders offered in the "Which order?" picker. */
export const RECENT_ORDERS = 20;
