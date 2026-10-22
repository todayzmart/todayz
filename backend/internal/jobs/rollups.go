package jobs

// registerrollups adds the rollups domain's River workers and periodic jobs.
// The work package that builds internal/rollups fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerrollups(r *Registry, d Deps) {}
