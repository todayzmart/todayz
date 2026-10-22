package main

import "github.com/go-chi/chi/v5"

// mountCredits mounts the credits domain's routes. Only the work package that builds
// internal/credits edits this file (D-50): build the domain's service from d and
// call its Register, e.g. credits.Register(r, credits.Deps{DB: d.DB, Clock: d.Clock}).
func mountCredits(r chi.Router, d *deps) {}
