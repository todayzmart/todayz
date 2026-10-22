package main

import "github.com/go-chi/chi/v5"

// mountRollups mounts the rollups domain's routes. Only the work package that builds
// internal/rollups edits this file (D-50): build the domain's service from d and
// call its Register, e.g. rollups.Register(r, rollups.Deps{DB: d.DB, Clock: d.Clock}).
func mountRollups(r chi.Router, d *deps) {}
