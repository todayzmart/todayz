package jobs

// registerorders declares the orders domain's job kinds.
// The work package that builds internal/orders fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registerorders(r *Registry, d Deps) {}
