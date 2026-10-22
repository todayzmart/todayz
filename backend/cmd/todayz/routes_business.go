package main

import "github.com/go-chi/chi/v5"

// mountBusiness mounts the business domain's routes. Only the work package that builds
// internal/business edits this file (D-50): build the domain's service from d and
// call its Register, e.g. business.Register(r, business.Deps{DB: d.DB, Clock: d.Clock}).
func mountBusiness(r chi.Router, d *deps) {}
