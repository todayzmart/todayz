package jobs

// registerpos declares the pos domain's job kinds.
// The work package that builds internal/pos fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registerpos(r *Registry, d Deps) {}
