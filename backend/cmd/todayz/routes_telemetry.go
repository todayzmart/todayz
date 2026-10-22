package main

import "github.com/go-chi/chi/v5"

// mountTelemetry mounts the telemetry domain's routes. Only the work package that builds
// internal/telemetry edits this file (D-50): build the domain's service from d and
// call its Register, e.g. telemetry.Register(r, telemetry.Deps{DB: d.DB, Clock: d.Clock}).
func mountTelemetry(r chi.Router, d *deps) {}
