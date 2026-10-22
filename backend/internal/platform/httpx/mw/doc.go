// Package mw holds the API middleware, one file per slot of the 02 §2.4
// pipeline (D-50). httpx.NewRouter mounts them in this order:
//
//	 1 requestid    W0-03  requestid.go   (server ULID; request logger; outermost)
//	 2 secheaders   W0-03  secheaders.go  (nosniff, no-referrer, CSP; no-store default on /v1)
//	 3 recover      W0-03  recover.go
//	 4 otel         W1-07  otel.go        (no-op until W1-07)
//	 5 clientip     W0-03  clientip.go
//	 6 accesslog    W1-07  accesslog.go   (minimal log from W0-03; W1-07 adds RED metrics)
//	 7 cors         W0-03  cors.go
//	 8 routepolicy  W0-03  routepolicy.go (the matched route's route.Policy into the context)
//	 9 bodylimit    W0-03  bodylimit.go   (1 MB; 10 MB only on Upload routes)
//	10 authn        W1-01  authn.go       (no-op until W1-01: every request is anonymous)
//	11 ratelimit    W1-01  ratelimit.go   (no-op until W1-01)
//	12 idempotency  W0-03  idempotency.go (only on routes that declare it)
//	13 validate     W0-04  validate.go    (hook; W0-04 plugs in kin-openapi)
//	14 authz        W1-01  authz.go       (per route; no-op until W1-01)
//
// Each owner edits only its own file. The order lives in httpx/router.go.
package mw
