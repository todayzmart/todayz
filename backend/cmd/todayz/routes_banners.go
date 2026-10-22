package main

import "github.com/go-chi/chi/v5"

// mountBanners mounts the banners domain's routes. Only the work package that builds
// internal/banners edits this file (D-50): build the domain's service from d and
// call its Register, e.g. banners.Register(r, banners.Deps{DB: d.DB, Clock: d.Clock}).
func mountBanners(r chi.Router, d *deps) {}
