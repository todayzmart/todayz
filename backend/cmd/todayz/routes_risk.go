package main

import "github.com/go-chi/chi/v5"

// mountRisk mounts the risk domain's routes. Only the work package that builds
// internal/risk edits this file (D-50): build the domain's service from d and
// call its Register, e.g. risk.Register(r, risk.Deps{DB: d.DB, Clock: d.Clock}).
func mountRisk(r chi.Router, d *deps) {}
