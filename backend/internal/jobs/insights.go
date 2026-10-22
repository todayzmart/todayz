package jobs

// registerinsights adds the insights domain's River workers and periodic jobs.
// The work package that builds internal/insights fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerinsights(r *Registry, d Deps) {}
