package main

import "github.com/go-chi/chi/v5"

// mountReferrals mounts the referrals domain's routes. Only the work package that builds
// internal/referrals edits this file (D-50): build the domain's service from d and
// call its Register, e.g. referrals.Register(r, referrals.Deps{DB: d.DB, Clock: d.Clock}).
func mountReferrals(r chi.Router, d *deps) {}
