package main

import "github.com/go-chi/chi/v5"

// mountOffers mounts the offers domain's routes. Only the work package that builds
// internal/offers edits this file (D-50): build the domain's service from d and
// call its Register, e.g. offers.Register(r, offers.Deps{DB: d.DB, Clock: d.Clock}).
func mountOffers(r chi.Router, d *deps) {}
