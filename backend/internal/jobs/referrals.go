package jobs

// registerreferrals adds the referrals domain's River workers and periodic jobs.
// The work package that builds internal/referrals fills it in: Add for each
// worker, r.Every for each periodic job. Keep it to this file.
func registerreferrals(r *Registry, d Deps) {}
