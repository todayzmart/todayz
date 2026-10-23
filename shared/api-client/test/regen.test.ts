// The client is derived from the bundle and nothing else (D-100 L9): the exports of
// src/gen/platform/platform.ts are exactly the bundle's platform operationIds, and
// running orval on a bundle with one operation removed (and one with a path
// parameter added) drops that function and encodes the parameter (M-3).
// Needs make gen (src/gen/openapi.json) first, like generated.test.ts.
// @vitest-environment node
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { generate } from 'orval';
import { afterAll, describe, expect, it } from 'vitest';

const pkgDir = resolve(import.meta.dirname, '..');
const bundle = join(pkgDir, 'src', 'gen', 'openapi.json');

interface Doc {
  paths: Record<string, Record<string, { operationId?: string; tags?: string[] }>>;
}

function operationIds(doc: Doc, tag: string): string[] {
  const ids: string[] = [];
  for (const item of Object.values(doc.paths)) {
    for (const op of Object.values(item)) {
      if (typeof op === 'object' && op.operationId && (op.tags ?? []).includes(tag)) ids.push(op.operationId);
    }
  }
  return ids.sort();
}

/** The operation functions a generated tags-split file exports (not the URL builders). */
function exportedOperations(src: string): string[] {
  return [...src.matchAll(/^export const (\w+) = async \(/gm)].map((m) => m[1] ?? '').sort();
}

describe('src/gen mirrors the bundle', () => {
  it('exports one function per platform operationId, no more', () => {
    const doc = JSON.parse(readFileSync(bundle, 'utf8')) as Doc;
    const src = readFileSync(join(pkgDir, 'src', 'gen', 'platform', 'platform.ts'), 'utf8');
    const ids = operationIds(doc, 'platform');
    expect(ids).toContain('getVersion');
    expect(exportedOperations(src)).toEqual(ids);
  });
});

describe('regenerating from a changed bundle', () => {
  const tmp = mkdtempSync(join(tmpdir(), 'todayz-api-client-'));
  afterAll(() => rmSync(tmp, { recursive: true, force: true }));

  it('drops a removed operation and encodes a path parameter', async () => {
    const doc = JSON.parse(readFileSync(bundle, 'utf8')) as Doc & { components?: unknown };
    expect(doc.paths['/version']).toBeDefined();
    delete doc.paths['/version'];
    doc.paths['/v1/me/orders/{oid}'] = {
      get: {
        operationId: 'getMyOrderForTest',
        tags: ['platform'],
        parameters: [{ name: 'oid', in: 'path', required: true, schema: { type: 'string', maxLength: 26 } }],
        responses: { '200': { description: 'ok', content: { 'application/json': { schema: { type: 'object' } } } } },
      } as never,
    };
    const spec = join(tmp, 'openapi.json');
    writeFileSync(spec, JSON.stringify(doc));

    const out = join(tmp, 'gen');
    // The same output options as orval.config.ts's `client` project.
    await generate(
      {
        input: { target: spec },
        output: {
          target: join(out, 'client.ts'),
          schemas: join(out, 'model'),
          mode: 'tags-split',
          client: 'fetch',
          httpClient: 'fetch',
          indexFiles: false,
          urlEncodeParameters: true,
          override: {
            mutator: { path: join(pkgDir, 'src', 'http.ts'), name: 'request' },
            fetch: { includeHttpResponseReturnType: false },
          },
        },
      },
      pkgDir,
    );

    const src = readFileSync(join(out, 'platform', 'platform.ts'), 'utf8');
    const ops = exportedOperations(src);
    expect(ops).not.toContain('getVersion');
    expect(ops).toContain('getHealthz');
    expect(ops).toContain('getMyOrderForTest');
    expect(src).toMatch(/encodeURIComponent\(String\(oid\)\)/);
    expect(src).not.toContain('${oid}');
  }, 60_000);
});
