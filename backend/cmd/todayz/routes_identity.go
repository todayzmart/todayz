package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/identity"
)

// mountIdentity mounts /v1/auth/* and /v1/me (W1-01). The service and the
// permission engine come from buildSecurity, which built them before the
// pipeline so the authenticator could use the session check.
func mountIdentity(r chi.Router, d *deps) {
	identity.Register(r, d.Identity, d.Authz)
}
