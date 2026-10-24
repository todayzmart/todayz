package jobs

// registerbaskets declares the baskets domain's job kinds. W2-14 has none:
// saved baskets have no timers and basket search is read-only.
func registerbaskets(r *Registry, d Deps) {}
