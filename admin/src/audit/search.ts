import type { AdminAuditSubjectType } from '@todayz/api-client/gen/model/adminAuditSubjectType';
import { AdminAuditSubjectType as SubjectTypes } from '@todayz/api-client/gen/model/adminAuditSubjectType';
import type { ListAdminAuditLogParams } from '@todayz/api-client/gen/model/listAdminAuditLogParams';
import { day, dayEnd, dayStart, text, ulid } from '../app/search';

export const SUBJECT_TYPES = Object.values(SubjectTypes) as readonly AdminAuditSubjectType[];

/** A-19 filters, kept in the URL. */
export interface AuditSearch {
  actorId?: string;
  action?: string;
  subjectType?: AdminAuditSubjectType;
  subjectId?: string;
  /** IST calendar days, YYYY-MM-DD. */
  from?: string;
  to?: string;
}

const ACTION = /^[a-z][a-z0-9_.]{0,62}$/;
const SUBJECT_ID = /^[A-Za-z0-9_.:-]{1,64}$/;

export function validateAuditSearch(raw: Record<string, unknown>): AuditSearch {
  const out: AuditSearch = {};
  const actor = ulid(raw.actorId);
  if (actor) out.actorId = actor;
  const action = text(raw.action);
  if (ACTION.test(action)) out.action = action;
  const type = text(raw.subjectType);
  if ((SUBJECT_TYPES as readonly string[]).includes(type)) out.subjectType = type as AdminAuditSubjectType;
  const sub = text(raw.subjectId);
  if (SUBJECT_ID.test(sub)) out.subjectId = sub;
  for (const k of ['from', 'to'] as const) {
    const d = day(raw[k]);
    if (d) out[k] = d;
  }
  return out;
}

export function auditParams(s: AuditSearch): Omit<ListAdminAuditLogParams, 'limit' | 'cursor'> {
  const p: Omit<ListAdminAuditLogParams, 'limit' | 'cursor'> = {};
  if (s.actorId) p.actorId = s.actorId;
  if (s.action) p.action = s.action;
  if (s.subjectType) p.subjectType = s.subjectType;
  if (s.subjectId) p.subjectId = s.subjectId;
  if (s.from) p.from = dayStart(s.from);
  if (s.to) p.to = dayEnd(s.to);
  return p;
}
