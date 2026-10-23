package jobs

// registersupport declares the support domain's job kinds.
// The work package that builds internal/support fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registersupport(r *Registry, d Deps) {}
