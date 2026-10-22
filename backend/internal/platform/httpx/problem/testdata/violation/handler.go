// Seeded fixture for TestScannerCatchesFixtureViolation: three unregistered
// literal codes and one registered one. Not compiled (testdata).
package violation

import (
	"net/http"

	p "github.com/todayz/backend/internal/platform/httpx/problem"
)

func handler(w http.ResponseWriter, r *http.Request) error {
	if r.URL.Path == "/a" {
		return p.New("no_such_code", "x")
	}
	if r.URL.Path == "/b" {
		return p.Wrap("another_missing", nil)
	}
	if r.URL.Path == "/c" {
		return &p.Error{Code: "literal_in_struct"}
	}
	return p.New("not_found", "registered, fine")
}
