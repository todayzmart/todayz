// Types for scripts/msw.mjs so test/msw.test.ts type-checks against it.
export interface SuccessResponse {
  status: number;
  empty?: boolean;
  skipped?: string;
  body?: unknown;
}

export interface Operation {
  id: string;
  method: 'get' | 'post' | 'put' | 'patch' | 'delete';
  path: string;
  summary: string;
  response: SuccessResponse | undefined;
}

export function mswPath(path: string): string;
export function literal(value: unknown): string;
export function comment(text: unknown): string;
export function firstExample(mediaType: unknown): { found: true; value: unknown } | undefined;
export function successResponse(operation: unknown): SuccessResponse | undefined;
export function operations(doc: unknown): Operation[];
export function renderHandlers(doc: unknown): string;
