package jobs

// registerpos adds the pos domain's River workers and periodic jobs.
// The work package that builds internal/pos fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerpos(r *Registry, d Deps) {}
