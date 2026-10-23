package mw

// Slot 13 · validate · hook from W0-03; W0-04 supplies the Validator
// (kin-openapi against the bundled api/openapi.yaml). It runs inline on
// every registered route, after chi has picked it and before idempotency
// (D-100): the validator reads chi's route pattern and parameters, and a
// refused request is never stored under its Idempotency-Key.

import (
	"net/http"

	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// Validator checks a request against the API contract. It returns nil when
// the request is valid, or an error problem.Write can send (normally
// validation_failed with Fields).
type Validator interface {
	ValidateRequest(r *http.Request) error
}

// ValidatorFunc adapts a function.
type ValidatorFunc func(r *http.Request) error

// ValidateRequest implements Validator.
func (f ValidatorFunc) ValidateRequest(r *http.Request) error { return f(r) }

// Validate runs v; a nil v passes every request through.
func Validate(v Validator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if v == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := v.ValidateRequest(r); err != nil {
				problem.Write(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
