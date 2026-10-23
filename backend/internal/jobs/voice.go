package jobs

// registervoice declares the voice domain's job kinds.
// The work package that builds internal/voice fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registervoice(r *Registry, d Deps) {}
