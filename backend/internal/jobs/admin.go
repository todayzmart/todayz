package jobs

// registeradmin declares the admin domain's job kinds.
// The work package that builds internal/admin fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registeradmin(r *Registry, d Deps) {}
