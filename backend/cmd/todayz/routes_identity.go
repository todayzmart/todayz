package main

import "github.com/go-chi/chi/v5"

// mountIdentity mounts the identity domain's routes. Only the work package that builds
// internal/identity edits this file (D-50): build the domain's service from d and
// call its Register, e.g. identity.Register(r, identity.Deps{DB: d.DB, Clock: d.Clock}).
func mountIdentity(r chi.Router, d *deps) {}
