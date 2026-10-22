package jobs

// registerbanners adds the banners domain's River workers and periodic jobs.
// The work package that builds internal/banners fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerbanners(r *Registry, d Deps) {}
