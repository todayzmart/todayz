import { text, ulid } from '../app/search';

/** A-18 view, kept in the URL: active or disabled accounts, and the account open beside the list. */
export interface AdminUsersSearch {
  /** Absent is `active`. */
  status?: 'disabled';
  /** The account shown in the side panel. */
  admin?: string;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateAdminUsersSearch(raw: Record<string, unknown>): AdminUsersSearch {
  const out: AdminUsersSearch = {};
  if (text(raw.status) === 'disabled') out.status = 'disabled';
  const id = ulid(raw.admin);
  if (id) out.admin = id;
  return out;
}
