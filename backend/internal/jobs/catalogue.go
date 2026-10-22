package jobs

// registercatalogue adds the catalogue domain's River workers and periodic jobs.
// The work package that builds internal/catalogue fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registercatalogue(r *Registry, d Deps) {}
