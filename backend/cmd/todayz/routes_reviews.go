package main

import "github.com/go-chi/chi/v5"

// mountReviews mounts the reviews domain's routes. Only the work package that builds
// internal/reviews edits this file (D-50): build the domain's service from d and
// call its Register, e.g. reviews.Register(r, reviews.Deps{DB: d.DB, Clock: d.Clock}).
func mountReviews(r chi.Router, d *deps) {}
