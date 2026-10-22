package jobs

// registersearch adds the search domain's River workers and periodic jobs.
// The work package that builds internal/search fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registersearch(r *Registry, d Deps) {}
