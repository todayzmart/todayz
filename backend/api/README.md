# backend/api: the API contract

OpenAPI 3.1, split per domain so parallel work packages never edit the same file (D-50, D-55).
`make gen` joins and bundles it; nothing generated is committed.

```
root.yaml               info, servers, security schemes, default security (W0-04; nobody adds paths here)
common/parameters.yaml  Limit, Cursor, IfMatch, IdempotencyKey, AcceptLanguage, Traceparent
common/headers.yaml     RequestId, ETag, RateLimit*, RetryAfter, IdempotentReplayed
common/responses.yaml   Problem (the `default` response of every operation)
schemas/common.yaml     Problem, Ulid, Paise, Credits, Timestamp, ServerNow, NextCursor, Slug, reason enums (D-60)
schemas/<domain>.yaml   map of schema name -> JSON Schema; names unique across the API
paths/<domain>.yaml     map of path -> path item; every operation tagged `<domain>`
problems/<domain>.yaml  problem codes (W0-03 registry; `make gen` builds gen/problems)
redocly.yaml            Redocly bundle + lint rules
openapi.yaml            THE BUNDLE: generated, git-ignored, never edited
```

## How a domain adds a path

1. Edit only `paths/<domain>.yaml`, `schemas/<domain>.yaml` and `problems/<domain>.yaml` for your
   domain (the contract lint fails a path file that tags another domain or a schema name another
   file already uses).
2. In `paths/<domain>.yaml` add `"/v1/...":` with one operation per method. Each operation has
   `operationId` (unique, camelCase), `tags: [<domain>]`, `summary`, a `default` response of
   `$ref: ../common/responses.yaml#/Problem`, and an example for every request body and every
   2xx response (`example` or `examples`). Public operations say `security: []`.
3. Follow 04 §1.2: no literal segment beside a path parameter at the same level, the same parameter
   name at the same position everywhere, only `{slug}` under `/v1/discover/stores/`, ULIDs only
   under `/v1/me`, `/v1/biz`, `/v1/admin`.
4. Request objects carry `additionalProperties: false` (an unknown field is `422 validation_failed`,
   with `fields` keyed by the prefixed path, `body.items[2].quantity`); an object that has to
   stay open and declares no properties says `x-open-object: true` (an object with properties
   may not). Every string in a request body has `maxLength`, an enum, a const, `format: binary`
   or a pattern that bounds its length, and every array has `maxItems` (D-100): the decoder
   refuses an array past it, more than 10,000 values or trailing data before validation. A
   request body is `application/json` only; an operation marked `x-upload: true` takes any
   other media type and may not take JSON (a non-JSON body is not validated; the handler
   streams it). A path never ends in `/`.
5. Every unsafe operation (POST, PUT, PATCH, DELETE) under `/v1/me`, `/v1/biz` or `/v1/admin`
   states `x-idempotency: none`, `optional` or `required` (04 §1.6); `optional` and `required`
   take the `../common/parameters.yaml#/IdempotencyKey` parameter, `none` does not. An operation
   that moves money says `x-money: true`, which requires `x-idempotency: required`; apigen holds
   the operationIds 04 §1.6 lists (`moneyOperations` in `internal/devtools/apigen/check.go`) and
   refuses one of them without `x-money` (an id not in the contract yet is fine; a domain that
   names its operation differently renames it there in the same change). Uploads say
   `x-upload: true`. The server reads these into the route policy (body limit, Idempotency-Key
   handling) and merges it with what the code declares: the stronger rule wins, and code weaker
   than the contract fails startup (D-100). The handler does not call `route.Handle` itself.
6. List the domain problem codes the operation can return under `x-problems: [code, ...]`; each
   must be registered in `problems/`.
7. Run `make gen`: it writes `gen/api/root.yaml`, bundles `api/openapi.yaml`, runs the contract
   lint (`apigen check`), generates `gen/api/<domain>/api.gen.go` (oapi-codegen types +
   strict chi server for the domain's tag) and `gen/api/<domain>/register.gen.go`, the TypeScript
   client and Zod schemas in `shared/api-client/src/gen/<domain>/` and the MSW handlers from the
   examples.
8. Wire the generated server in `cmd/todayz/routes_<domain>.go` through the generated
   `Register`, which builds the strict handler with `openapi.StrictOptions()` (every path and
   body error is a problem, never a bare string). The route coverage test (`cmd/todayz`,
   `TestRoutesAreInTheContract`) fails on a chi route the spec does not know and on a contract
   operation without a route once its domain has landed.

## Lints (`make check`, SC-PL-01, SC-PL-02)

`redocly lint` on the bundle, then `apigen check`: duplicate method + path across files, duplicate
operationId or schema name, an operation without the `default` problem response, a request or 2xx
response without an example, an example that does not validate against its schema, a request
object open to unknown fields or `x-open-object` on an object with properties, a request-body
string without a length bound or array without `maxItems`, a `$ref` into another domain's
schema file, `x-idempotency` with a value other than `none|optional|required`, `optional` or
`required` without the `Idempotency-Key` parameter (or the reverse, or `none` with it), an
unsafe operation under `/v1/me`, `/v1/biz` or `/v1/admin` with no `x-idempotency`, `x-money`
without `x-idempotency: required`, a 04 §1.6 money operationId without `x-money`, a request body
that is not `application/json` on an operation without `x-upload` (or JSON on one with it), a
pattern that does not bound a string (top-level alternation outside a group, or an escaped `\$`
at the end), a schema keyword the validator does not run (`if`, `then`, `else`,
`dependentRequired`, `dependentSchemas`, `contains`, `minContains`, `maxContains`,
`patternProperties`, `propertyNames`, `unevaluatedProperties`, `unevaluatedItems`,
`prefixItems`, `$dynamicRef`, `$dynamicAnchor`, `contentSchema`), an `x-problems` code that is
not registered, a path that breaks 04 §1.2, ends in `/` or starts outside the namespaces
(`/v1/biz/businesses` itself is allowed, where a business is created), a
path file whose operations are tagged with another domain. Fixtures under
`internal/devtools/apigen/testdata/` prove each lint fires.
