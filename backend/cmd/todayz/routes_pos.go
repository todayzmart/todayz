package main

import "github.com/go-chi/chi/v5"

// mountPos mounts the pos domain's routes. Only the work package that builds
// internal/pos edits this file (D-50): build the domain's service from d and
// call its Register, e.g. pos.Register(r, pos.Deps{DB: d.DB, Clock: d.Clock}).
func mountPos(r chi.Router, d *deps) {}
