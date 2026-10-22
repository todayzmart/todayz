package jobs

// registerrisk adds the risk domain's River workers and periodic jobs.
// The work package that builds internal/risk fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerrisk(r *Registry, d Deps) {}
