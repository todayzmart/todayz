package main

import "github.com/go-chi/chi/v5"

// mountVoice mounts the voice domain's routes. Only the work package that builds
// internal/voice edits this file (D-50): build the domain's service from d and
// call its Register, e.g. voice.Register(r, voice.Deps{DB: d.DB, Clock: d.Clock}).
func mountVoice(r chi.Router, d *deps) {}
