// orval config for @todayz/api-client (W0-04). `make gen` (mk/25-contract.mk) runs
// scripts/gen.mjs, which points TODAYZ_OPENAPI at the bundled backend/api/openapi.yaml
// and runs orval with this file, then writes the MSW handlers from the examples.
//
//   src/gen/<domain>/<domain>.ts        fetch client, one function per operationId,
//                                       every call through request() in src/http.ts
//   src/gen/model/                      TypeScript types, one file per schema
//   src/gen/<domain>/<domain>.zod.ts    Zod schemas for params, bodies and responses
//
// Everything under src/gen is generated and git-ignored.
import { defineConfig } from 'orval';

const input = { target: process.env['TODAYZ_OPENAPI'] ?? '../../backend/api/openapi.yaml' };

export default defineConfig({
  client: {
    input,
    output: {
      target: 'src/gen/client.ts',
      schemas: 'src/gen/model',
      mode: 'tags-split',
      client: 'fetch',
      httpClient: 'fetch',
      indexFiles: false,
      // Every path parameter goes through encodeURIComponent, so an id can never
      // add a segment; request() refuses a dot segment as a second wall (D-100 M-3).
      urlEncodeParameters: true,
      override: {
        mutator: { path: 'src/http.ts', name: 'request' },
        // request() resolves the decoded body and throws ApiError for everything
        // else, so an operation's type is its 2xx body, not {data, status, headers}.
        fetch: { includeHttpResponseReturnType: false },
      },
    },
  },
  zod: {
    input,
    output: {
      target: 'src/gen/zod.ts',
      mode: 'tags-split',
      client: 'zod',
      fileExtension: '.zod.ts',
      indexFiles: false,
      urlEncodeParameters: true,
    },
  },
});
