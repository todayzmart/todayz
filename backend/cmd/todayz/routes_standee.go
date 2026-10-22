package main

import "github.com/go-chi/chi/v5"

// mountStandee mounts the standee domain's routes. Only the work package that builds
// internal/standee edits this file (D-50): build the domain's service from d and
// call its Register, e.g. standee.Register(r, standee.Deps{DB: d.DB, Clock: d.Clock}).
func mountStandee(r chi.Router, d *deps) {}
