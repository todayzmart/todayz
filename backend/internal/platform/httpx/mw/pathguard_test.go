package mw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPathGuard (D-100, security review H-1): a path two routers could read
// differently never reaches the router.
func TestPathGuard(t *testing.T) {
	var reached string
	h := PathGuard()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	for target, want := range map[string]int{
		"/v1/me/orders/01ARZ3NDEKTSV4RRFFQ69G5FAV": 204,
		"/v1/me/orders/a%20b":                      204, // canonical encoding
		"/v1/me/orders/a%41":                       204, // an over-encoded letter is still one path
		"/v1/discover/search?q=%2Fx":               204, // the query is not the path
		"/v1/me/orders/a%2F%22":                    400, // encoded slash
		"/v1/me/orders/a%2f%22":                    400,
		"/v1/me/orders/a%2F\"":                     400, // the H-1 proof: %2F plus an invalid escape
		"/v1/me/orders/a%2F\xc3\xa9":               400,
		"/v1/me/orders/a\"b":                       400, // not the canonical encoding
		"/v1/me/orders/a%5Cb":                      400, // encoded backslash
		"/v1/me/orders/a\\b":                       400,
		// An invalid escape such as %zz never reaches a handler: net/http refuses it while parsing the request line.
	} {
		reached = ""
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, want, rec.Code, target)
		if want == 204 {
			require.NotEmpty(t, reached, target)
		} else {
			require.Empty(t, reached, "refused before the router: "+target)
			require.Contains(t, rec.Body.String(), `"code":"bad_request"`)
		}
	}
}
