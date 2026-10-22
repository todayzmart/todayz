package main

import "github.com/go-chi/chi/v5"

// mountNotify mounts the notify domain's routes. Only the work package that builds
// internal/notify edits this file (D-50): build the domain's service from d and
// call its Register, e.g. notify.Register(r, notify.Deps{DB: d.DB, Clock: d.Clock}).
func mountNotify(r chi.Router, d *deps) {}
