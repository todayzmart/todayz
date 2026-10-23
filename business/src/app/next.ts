import { safeNext as shared, validateNextSearch as validate } from '@todayz/auth';

/** Gate routes that must never be a `?next=` target (they would loop). */
const BLOCKED = ['/sign-in', '/choose-shop'] as const;

/** `?next=` after sign-in: a path on this app only, never another origin (see `@todayz/auth`). */
export const safeNext = (next: unknown): string => shared(next, BLOCKED);

export const validateNextSearch = (search: Record<string, unknown>): { next?: string } => validate(search, BLOCKED);
