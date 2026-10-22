package jobs

// registeroffers adds the offers domain's River workers and periodic jobs.
// The work package that builds internal/offers fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registeroffers(r *Registry, d Deps) {}
