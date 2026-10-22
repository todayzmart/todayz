package main

import "github.com/go-chi/chi/v5"

// mountAI mounts the ai domain's routes. Only the work package that builds
// internal/ai edits this file (D-50): build the domain's service from d and
// call its Register, e.g. ai.Register(r, ai.Deps{DB: d.DB, Clock: d.Clock}).
func mountAI(r chi.Router, d *deps) {}
