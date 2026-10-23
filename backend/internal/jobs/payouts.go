package jobs

// registerpayouts declares the payouts domain's job kinds.
// The work package that builds internal/payouts fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registerpayouts(r *Registry, d Deps) {}
