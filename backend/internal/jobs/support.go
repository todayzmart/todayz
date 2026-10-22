package jobs

// registersupport adds the support domain's River workers and periodic jobs.
// The work package that builds internal/support fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registersupport(r *Registry, d Deps) {}
