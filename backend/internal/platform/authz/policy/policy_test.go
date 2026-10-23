package policy

import (
	"testing"

	"github.com/todayz/backend/internal/platform/authz"
)

func TestRegisterTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering the identity table twice did not panic")
		}
	}()
	Register(authz.Table{Domain: "identity"})
}
