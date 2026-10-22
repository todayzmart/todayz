package main

import "github.com/go-chi/chi/v5"

// mountSearch mounts the search domain's routes. Only the work package that builds
// internal/search edits this file (D-50): build the domain's service from d and
// call its Register, e.g. search.Register(r, search.Deps{DB: d.DB, Clock: d.Clock}).
func mountSearch(r chi.Router, d *deps) {}
