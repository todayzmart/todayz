package jobs

// registeradmin adds the admin domain's River workers and periodic jobs.
// The work package that builds internal/admin fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registeradmin(r *Registry, d Deps) {}
