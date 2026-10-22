package jobs

// registertelemetry adds the telemetry domain's River workers and periodic jobs.
// The work package that builds internal/telemetry fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registertelemetry(r *Registry, d Deps) {}
