package main

import "github.com/go-chi/chi/v5"

// mountOrders mounts the orders domain's routes. Only the work package that builds
// internal/orders edits this file (D-50): build the domain's service from d and
// call its Register, e.g. orders.Register(r, orders.Deps{DB: d.DB, Clock: d.Clock}).
func mountOrders(r chi.Router, d *deps) {}
