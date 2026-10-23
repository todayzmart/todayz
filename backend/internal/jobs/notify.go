package jobs

// registernotify declares the notify domain's job kinds.
// The work package that builds internal/notify fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registernotify(r *Registry, d Deps) {}
