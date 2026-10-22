package jobs

// registeridentity adds the identity domain's River workers and periodic jobs.
// The work package that builds internal/identity fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registeridentity(r *Registry, d Deps) {}
