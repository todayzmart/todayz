package main

import "github.com/go-chi/chi/v5"

// mountCatalogue mounts the catalogue domain's routes. Only the work package that builds
// internal/catalogue edits this file (D-50): build the domain's service from d and
// call its Register, e.g. catalogue.Register(r, catalogue.Deps{DB: d.DB, Clock: d.Clock}).
func mountCatalogue(r chi.Router, d *deps) {}
