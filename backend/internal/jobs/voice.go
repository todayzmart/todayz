package jobs

// registervoice adds the voice domain's River workers and periodic jobs.
// The work package that builds internal/voice fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registervoice(r *Registry, d Deps) {}
