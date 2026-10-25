package jobs

// registerinsights declares the insights domain's job kinds: none. W3-03
// computes Demand insights at read time from W3-07's rollups (D-99), so there
// is nothing to schedule; W3-11's coach brings its own job when it lands.
func registerinsights(r *Registry, d Deps) {}
