package jobs

// registerorders adds the orders domain's River workers and periodic jobs.
// The work package that builds internal/orders fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerorders(r *Registry, d Deps) {}
