package main

import (
	"github.com/go-chi/chi/v5"

	teldomain "github.com/todayz/backend/internal/telemetry"
)

// mountTelemetry mounts POST /v1/rum, POST /v1/events and, behind the /v1/test/clock gate, the slow
// test routes (W1-07). A catalogue that does not load is a build error: the API refuses to start.
func mountTelemetry(r chi.Router, d *deps) {
	_, _, err := teldomain.Register(r, teldomain.Deps{
		DB: d.DB, Clock: d.Clock, Metrics: d.Metrics,
		Env: d.Config.Env, APIAddr: d.Config.APIAddr, TestRoutes: d.TestClock != nil,
	})
	if err != nil {
		panic("telemetry routes: " + err.Error())
	}
}
