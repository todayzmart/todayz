// @todayz/api-client: the hand-written half. The generated operations live under
// `@todayz/api-client/gen/<domain>/<domain>` (types in `gen/model/<Name>`, Zod in
// `gen/<domain>/<domain>.zod`) and the MSW handlers under `@todayz/api-client/mocks`.
export { ApiError, checkPath, configure, fallbackProblem, request } from './http';
export type { ApiClientConfig, ApiErrorJSON, Problem, RequestFn, RequestOptions } from './http';
