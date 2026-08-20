// Package httpapi exposes the schema registry over HTTP using the standard
// library router (Go 1.22+ method+path patterns). The same mux is used by the
// server entry point and by tests, so handler behavior is verified end-to-end.
package httpapi

import (
	"net/http"

	"task102-schemaregistry/internal/registry"
)

// NewMux returns a router wired to the registry.
func NewMux(r *registry.Registry) http.Handler {
	mux := http.NewServeMux()
	h := &Handler{r: r}

	// liveness / readiness / discovery
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /ready", h.Ready)
	mux.HandleFunc("GET /", h.Index)

	// subjects
	mux.HandleFunc("GET /subjects", h.ListSubjects)
	mux.HandleFunc("GET /subjects/{subject}", h.SubjectInfo)
	mux.HandleFunc("POST /subjects/{subject}/versions", h.Register)
	mux.HandleFunc("DELETE /subjects/{subject}", h.DeleteSubject)
	mux.HandleFunc("GET /subjects/{subject}/versions", h.ListVersions)
	mux.HandleFunc("GET /subjects/{subject}/versions/{version}", h.GetSchema)
	mux.HandleFunc("GET /subjects/{subject}/versions/latest", h.GetLatest)
	mux.HandleFunc("DELETE /subjects/{subject}/versions/{version}", h.DeleteVersion)

	// per-version extras
	mux.HandleFunc("GET /subjects/{subject}/versions/{version}/fingerprint", h.GetFingerprint)

	// compatibility checks
	mux.HandleFunc("POST /compatibility/subjects/{subject}/versions/latest", h.CheckCompat)
	mux.HandleFunc("POST /compatibility/subjects/{subject}/versions/{version}", h.CheckCompatVersion)

	// message validation / canonicalization
	mux.HandleFunc("POST /subjects/{subject}/validate", h.Validate)
	mux.HandleFunc("POST /subjects/{subject}/canonicalize", h.Canonicalize)

	// inline schema operations (no persistence)
	mux.HandleFunc("POST /schemas/validate", h.ValidateInline)
	mux.HandleFunc("POST /schemas/fingerprint", h.Fingerprint)
	mux.HandleFunc("POST /schemas/compatibility", h.CompatInline)
	mux.HandleFunc("POST /schemas/normalize", h.Normalize)
	mux.HandleFunc("POST /schemas/diff", h.Diff)

	// compatibility config
	mux.HandleFunc("GET /config", h.GetGlobalConfig)
	mux.HandleFunc("PUT /config", h.SetGlobalConfig)
	mux.HandleFunc("GET /config/{subject}", h.GetConfig)
	mux.HandleFunc("PUT /config/{subject}", h.SetConfig)

	// stats / audit / export
	mux.HandleFunc("GET /subjects/{subject}/stats", h.Stats)
	mux.HandleFunc("GET /subjects/{subject}/audit", h.Audit)
	mux.HandleFunc("GET /subjects/{subject}/export", h.Export)
	mux.HandleFunc("GET /overview", h.Overview)

	return mux
}
