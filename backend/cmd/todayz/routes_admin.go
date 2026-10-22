package main

import "github.com/go-chi/chi/v5"

// mountAdmin mounts the admin domain's routes. Only the work package that builds
// internal/admin edits this file (D-50): build the domain's service from d and
// call its Register, e.g. admin.Register(r, admin.Deps{DB: d.DB, Clock: d.Clock}).
func mountAdmin(r chi.Router, d *deps) {}
