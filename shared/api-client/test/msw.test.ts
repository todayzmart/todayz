import { describe, expect, it } from 'vitest';

import { comment, firstExample, literal, mswPath, operations, renderHandlers, successResponse } from '../scripts/msw.mjs';

const doc = {
  openapi: '3.1.0',
  paths: {
    '/v1/me/orders/{oid}': {
      get: {
        operationId: 'getMyOrder',
        summary: 'One order',
        responses: {
          '200': { content: { 'application/json': { examples: { ready: { value: { id: 'o1', status: 'ready' } } } } } },
          '404': { content: { 'application/problem+json': { example: { code: 'not_found' } } } },
        },
      },
      delete: { operationId: 'cancelMyOrder', responses: { '204': { description: 'gone' } } },
    },
    '/v1/biz/businesses/{bid}/items/{iid}/photo': {
      get: {
        operationId: 'getItemPhoto',
        responses: { '200': { content: { 'image/webp': { schema: { type: 'string', format: 'binary' } } } } },
      },
    },
    '/v1/admin/things': {
      post: {
        operationId: 'createThing',
        responses: {
          '201': { content: { 'application/json': { example: { id: 't1' } } } },
          '200': { content: { 'application/json': { example: { wrong: true } } } },
        },
      },
      put: { operationId: 'noExample', responses: { '200': { content: { 'application/json': {} } } } },
    },
  },
};

describe('mswPath', () => {
  it('turns OpenAPI templates into MSW params on any origin', () => {
    expect(mswPath('/v1/me/orders/{oid}')).toBe('*/v1/me/orders/:oid');
    expect(mswPath('/v1/biz/businesses/{bid}/items/{iid}')).toBe('*/v1/biz/businesses/:bid/items/:iid');
    expect(mswPath('/healthz')).toBe('*/healthz');
  });
});

describe('firstExample', () => {
  it('prefers example, then the first of examples, else nothing', () => {
    expect(firstExample({ example: 1 })).toEqual({ found: true, value: 1 });
    expect(firstExample({ examples: { a: { value: 'A' }, b: { value: 'B' } } })).toEqual({ found: true, value: 'A' });
    expect(firstExample({ examples: {} })).toBeUndefined();
    expect(firstExample({})).toBeUndefined();
    expect(firstExample(undefined)).toBeUndefined();
  });
});

describe('successResponse', () => {
  it('picks the lowest 2xx and its JSON example', () => {
    expect(successResponse(doc.paths['/v1/admin/things'].post)).toEqual({ status: 200, body: { wrong: true } });
    expect(successResponse(doc.paths['/v1/me/orders/{oid}'].get)).toEqual({ status: 200, body: { id: 'o1', status: 'ready' } });
  });
  it('marks empty, non-JSON and example-less responses', () => {
    expect(successResponse(doc.paths['/v1/me/orders/{oid}'].delete)).toEqual({ status: 204, empty: true });
    expect(successResponse(doc.paths['/v1/biz/businesses/{bid}/items/{iid}/photo'].get)).toEqual({ status: 200, skipped: 'image/webp' });
    expect(successResponse(doc.paths['/v1/admin/things'].put)).toEqual({ status: 200, skipped: 'no example' });
    expect(successResponse({ responses: { '404': {} } })).toBeUndefined();
  });
});

describe('operations', () => {
  it('lists every method of every path in path order', () => {
    expect(operations(doc).map((o) => `${o.method} ${o.path}`)).toEqual([
      'post /v1/admin/things',
      'put /v1/admin/things',
      'get /v1/biz/businesses/{bid}/items/{iid}/photo',
      'get /v1/me/orders/{oid}',
      'delete /v1/me/orders/{oid}',
    ]);
  });
  it('refuses an operation without an operationId', () => {
    expect(() => operations({ paths: { '/x': { get: {} } } })).toThrow('GET /x has no operationId');
  });
  it('refuses an operationId that is not an identifier, since it becomes an export', () => {
    for (const id of ['get-x', 'a b', "x'; process.exit(1); //", '1x', 'x*/', 'getX\nexport const y = 1']) {
      expect(() => operations({ paths: { '/x': { get: { operationId: id, responses: {} } } } }), id).toThrow('is not an identifier');
    }
  });
});

describe('literal and comment (security L-4)', () => {
  it('quotes strings with JSON.stringify and never interpolates them raw', () => {
    expect(literal("it's")).toBe('"it\'s"');
    expect(literal('a"b')).toBe('"a\\"b"');
    expect(literal('line\nbreak')).toBe('"line\\nbreak"');
  });
  it('keeps a comment from being closed or broken', () => {
    expect(comment('a */ b')).toBe('a * / b');
    expect(comment('a\nb\r\nc d')).toBe('a b  c d');
    expect(comment(undefined)).toBe('undefined');
  });
});

describe('renderHandlers with hostile text', () => {
  const hostile = {
    paths: {
      "/v1/me/x'y": {
        get: {
          operationId: 'quoteInPath',
          summary: 'closes */ the comment\nexport const injected = 1;',
          responses: { '200': { content: { 'application/json': { example: { s: "*/ '\"\n" } } } } },
        },
      },
      '/v1/me/noJson*/': {
        get: { operationId: 'skippedOne', responses: { '200': { content: { 'image/webp': {} } } } },
      },
    },
  };
  const src = renderHandlers(hostile);

  it('JSON-quotes every path and body and defuses comment closers', () => {
    expect(src).toContain('path: "/v1/me/x\'y"');
    expect(src).toContain('http.get("*/v1/me/x\'y", ');
    expect(src).toContain('/** GET /v1/me/x\'y: closes * / the comment export const injected = 1; */');
    expect(src).toContain('//   GET /v1/me/noJson* / (skippedOne): image/webp');
    expect(src).not.toMatch(/^export const injected/m);
    // The example body is a JSON literal, so its `*/` and quotes stay inside a string.
    expect(src).toContain('"s": "*/ \'\\"\\n"');
  });
});

describe('renderHandlers', () => {
  const src = renderHandlers(doc);

  it('writes one named handler per JSON operation and lists the rest', () => {
    expect(src).toContain('export const getMyOrder = http.get("*/v1/me/orders/:oid", () => HttpResponse.json(examples.getMyOrder.body, { status: 200 }));');
    expect(src).toContain('export const cancelMyOrder = http.delete("*/v1/me/orders/:oid", () => new HttpResponse(null, { status: 204 }));');
    expect(src).toContain('export const createThing = http.post("*/v1/admin/things", () => HttpResponse.json(examples.createThing.body, { status: 200 }));');
    expect(src).toContain('export const handlers = [createThing, getMyOrder, cancelMyOrder];');
    expect(src).toContain('//   GET /v1/biz/businesses/{bid}/items/{iid}/photo (getItemPhoto): image/webp');
    expect(src).toContain('//   PUT /v1/admin/things (noExample): no example');
    expect(src).not.toContain('export const getItemPhoto');
    expect(src).toContain('/** GET /v1/me/orders/{oid}: One order */');
  });

  it('embeds the example bodies verbatim', () => {
    expect(src).toContain('"status": "ready"');
    expect(src).toContain('cancelMyOrder: { method: "DELETE", path: "/v1/me/orders/{oid}", status: 204, body: undefined }');
    expect(src.startsWith('// Code generated by')).toBe(true);
  });

  it('omits the skipped list when nothing is skipped', () => {
    const only = { paths: { '/healthz': { get: { operationId: 'getHealthz', responses: { '200': { content: { 'application/json': { example: { status: 'ok' } } } } } } } } };
    const out = renderHandlers(only);
    expect(out).not.toContain('Without a handler');
    expect(out).toContain('export const handlers = [getHealthz];');
  });
});
