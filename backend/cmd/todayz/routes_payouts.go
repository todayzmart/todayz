package main

import "github.com/go-chi/chi/v5"

// mountPayouts mounts the payouts domain's routes. Only the work package that builds
// internal/payouts edits this file (D-50): build the domain's service from d and
// call its Register, e.g. payouts.Register(r, payouts.Deps{DB: d.DB, Clock: d.Clock}).
func mountPayouts(r chi.Router, d *deps) {}
