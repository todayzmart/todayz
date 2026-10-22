package jobs

// registerstandee adds the standee domain's River workers and periodic jobs.
// The work package that builds internal/standee fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerstandee(r *Registry, d Deps) {}
