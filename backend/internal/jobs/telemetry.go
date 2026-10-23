package jobs

// registertelemetry declares the telemetry domain's job kinds.
// The work package that builds internal/telemetry fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registertelemetry(r *Registry, d Deps) {}
