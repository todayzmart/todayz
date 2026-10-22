package jobs

// registercoach adds the coach domain's River workers and periodic jobs.
// The work package that builds internal/coach fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registercoach(r *Registry, d Deps) {}
