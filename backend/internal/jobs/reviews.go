package jobs

// registerreviews adds the reviews domain's River workers and periodic jobs.
// The work package that builds internal/reviews fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerreviews(r *Registry, d Deps) {}
