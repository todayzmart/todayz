package main

import "github.com/go-chi/chi/v5"

// mountInsights mounts the insights domain's routes. Only the work package that builds
// internal/insights edits this file (D-50): build the domain's service from d and
// call its Register, e.g. insights.Register(r, insights.Deps{DB: d.DB, Clock: d.Clock}).
func mountInsights(r chi.Router, d *deps) {}
