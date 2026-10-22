package mw

// Slot 3 · recover · owner W0-03.

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Recover turns a panic into 500 internal and logs it with the stack through
// the request logger (log when there is none). http.ErrAbortHandler is
// re-raised (the server aborts the connection). W1-07 adds the Sentry capture
// and the panic metric here.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				telemetry.LoggerOr(r.Context(), log).ErrorContext(r.Context(), "panic",
					slog.Any("panic", v), slog.String("stack", string(debug.Stack())))
				problem.Write(w, r, problem.New("internal", ""))
			}()
			next.ServeHTTP(w, r)
		})
	}
}
