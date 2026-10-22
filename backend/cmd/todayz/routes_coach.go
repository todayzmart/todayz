package main

import "github.com/go-chi/chi/v5"

// mountCoach mounts the coach domain's routes. Only the work package that builds
// internal/coach edits this file (D-50): build the domain's service from d and
// call its Register, e.g. coach.Register(r, coach.Deps{DB: d.DB, Clock: d.Clock}).
func mountCoach(r chi.Router, d *deps) {}
