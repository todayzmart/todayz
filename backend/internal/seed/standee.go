package seed

import "context"

// seedstandee writes the standee domain's seed rows for profile p (dev, demo or
// test) through d.DB.Q(ctx), inside the seed transaction. The work package
// that builds internal/standee fills it in. Keep it to this file, and keep it
// deterministic: fixed ids and the seed clock, never time.Now or rand.
func seedstandee(ctx context.Context, d Deps, p Profile) error { return nil }
