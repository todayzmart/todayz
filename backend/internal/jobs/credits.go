package jobs

// registercredits adds the credits domain's River workers and periodic jobs.
// The work package that builds internal/credits fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registercredits(r *Registry, d Deps) {}
