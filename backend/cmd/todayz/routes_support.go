package main

import "github.com/go-chi/chi/v5"

// mountSupport mounts the support domain's routes. Only the work package that builds
// internal/support edits this file (D-50): build the domain's service from d and
// call its Register, e.g. support.Register(r, support.Deps{DB: d.DB, Clock: d.Clock}).
func mountSupport(r chi.Router, d *deps) {}
