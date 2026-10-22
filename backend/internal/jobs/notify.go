package jobs

// registernotify adds the notify domain's River workers and periodic jobs.
// The work package that builds internal/notify fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registernotify(r *Registry, d Deps) {}
