package jobs

// registerbusiness adds the business domain's River workers and periodic jobs.
// The work package that builds internal/business fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerbusiness(r *Registry, d Deps) {}
