package jobs

// registerpayouts adds the payouts domain's River workers and periodic jobs.
// The work package that builds internal/payouts fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerpayouts(r *Registry, d Deps) {}
