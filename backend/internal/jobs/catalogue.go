package jobs

// registercatalogue declares the catalogue domain's job kinds.
// The work package that builds internal/catalogue fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registercatalogue(r *Registry, d Deps) {}
