package jobs

// registerai adds the ai domain's River workers and periodic jobs.
// The work package that builds internal/ai fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerai(r *Registry, d Deps) {}
