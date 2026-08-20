package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"task102-schemaregistry/internal/registry"
	"task102-schemaregistry/internal/store"
)

// Handler holds the registry used by all endpoints.
type Handler struct {
	r *registry.Registry
}

// errBody is the structured error response.
type errBody struct {
	Error errDetail `json:"error"`
}
type errDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Version int    `json:"version,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg, field string, version int) {
	writeJSON(w, status, errBody{Error: errDetail{Code: code, Message: msg, Field: field, Version: version}})
}

// classifyErr maps a registry error to an HTTP status and error code.
func classifyErr(err error) (int, string) {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound, "not_found"
	}
	var nl *registry.ErrNotLatest
	if errors.As(err, &nl) {
		return http.StatusBadRequest, "not_latest"
	}
	var ce *registry.CompatError
	if errors.As(err, &ce) {
		return http.StatusConflict, "incompatible"
	}
	return http.StatusBadRequest, "bad_request"
}

// compatErrDetail extracts the field/version from a CompatError for the error
// response body.
func compatErrDetail(err error) (field string, version int) {
	var ce *registry.CompatError
	if errors.As(err, &ce) {
		return ce.Field, ce.Version
	}
	return "", 0
}

// readBody reads a JSON request body, returning a 400 on read error.
func readBody(w http.ResponseWriter, req *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(req.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "读取请求体失败", "", 0)
		return nil, false
	}
	return b, true
}

// Health reports liveness.
func (h *Handler) Health(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready reports readiness.
func (h *Handler) Ready(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Index returns service discovery information.
func (h *Handler) Index(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"service": "schema-registry",
		"version": "1",
		"endpoints": []string{
			"GET /health",
			"GET /ready",
			"GET /subjects",
			"POST /subjects/{subject}/versions",
			"GET /subjects/{subject}/versions/{version}",
			"POST /compatibility/subjects/{subject}/versions/latest",
			"POST /subjects/{subject}/validate",
			"POST /schemas/validate",
			"POST /schemas/fingerprint",
			"POST /schemas/compatibility",
			"POST /schemas/normalize",
			"POST /schemas/diff",
			"GET /config/{subject}",
			"PUT /config/{subject}",
		},
	})
}

// Register handles POST /subjects/{subject}/versions.
func (h *Handler) Register(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	res, err := h.r.Register(subject, body)
	if err != nil {
		status, code := classifyErr(err)
		field, version := compatErrDetail(err)
		writeErr(w, status, code, err.Error(), field, version)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GetSchema handles GET /subjects/{subject}/versions/{version}.
func (h *Handler) GetSchema(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	version, err := h.r.LookupVersion(subject, req.PathValue("version"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	view, err := h.r.GetSchema(subject, version)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// GetLatest handles GET /subjects/{subject}/versions/latest.
func (h *Handler) GetLatest(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	view, err := h.r.GetSchema(subject, 0)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// ListVersions handles GET /subjects/{subject}/versions.
func (h *Handler) ListVersions(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	vs, err := h.r.ListVersions(subject)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"subject": subject, "versions": vs})
}

// ListSubjects handles GET /subjects.
func (h *Handler) ListSubjects(w http.ResponseWriter, req *http.Request) {
	subs, err := h.r.ListSubjects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"subjects": subs})
}

// SubjectInfo handles GET /subjects/{subject}.
func (h *Handler) SubjectInfo(w http.ResponseWriter, req *http.Request) {
	info, err := h.r.SubjectInfo(req.PathValue("subject"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// DeleteSubject handles DELETE /subjects/{subject}.
func (h *Handler) DeleteSubject(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	if err := h.r.DeleteSubject(subject); err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"subject": subject, "deleted": "true"})
}

// DeleteVersion handles DELETE /subjects/{subject}/versions/{version}.
func (h *Handler) DeleteVersion(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	version, err := h.r.LookupVersion(subject, req.PathValue("version"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	if err := h.r.DeleteVersion(subject, version); err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"subject": subject, "version": version, "deleted": "true"})
}

// GetFingerprint handles GET /subjects/{subject}/versions/{version}/fingerprint.
func (h *Handler) GetFingerprint(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	version, err := h.r.LookupVersion(subject, req.PathValue("version"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	fp, err := h.r.GetFingerprint(subject, version)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"subject": subject, "version": version, "fingerprint": fp})
}

// CheckCompat handles POST /compatibility/subjects/{subject}/versions/latest.
func (h *Handler) CheckCompat(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	res, err := h.r.CheckCompat(subject, body)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// CheckCompatVersion handles POST /compatibility/subjects/{subject}/versions/{version}.
func (h *Handler) CheckCompatVersion(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	version, err := h.r.LookupVersion(subject, req.PathValue("version"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	res, err := h.r.CheckCompatVersion(subject, version, body)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Validate handles POST /subjects/{subject}/validate?version=N|latest.
func (h *Handler) Validate(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	version, err := h.r.LookupVersion(subject, req.URL.Query().Get("version"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	res, err := h.r.Validate(subject, version, body)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Canonicalize handles POST /subjects/{subject}/canonicalize?version=N|latest.
func (h *Handler) Canonicalize(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	version, err := h.r.LookupVersion(subject, req.URL.Query().Get("version"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	res, err := h.r.Canonicalize(subject, version, body)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Fingerprint handles POST /schemas/fingerprint.
func (h *Handler) Fingerprint(w http.ResponseWriter, req *http.Request) {
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	fp, err := h.r.FingerprintCandidate(body)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"fingerprint": fp})
}

// Normalize handles POST /schemas/normalize.
func (h *Handler) Normalize(w http.ResponseWriter, req *http.Request) {
	body, ok := readBody(w, req)
	if !ok {
		return
	}
	res, err := h.r.NormalizeSchema(body)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ValidateInline handles POST /schemas/validate.
func (h *Handler) ValidateInline(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Schema  json.RawMessage `json:"schema"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON", "", 0)
		return
	}
	res, err := h.r.ValidateInline(body.Schema, body.Message)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// CompatInline handles POST /schemas/compatibility.
func (h *Handler) CompatInline(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Old  json.RawMessage `json:"old"`
		New  json.RawMessage `json:"new"`
		Mode string          `json:"mode"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON", "", 0)
		return
	}
	res, err := h.r.CompatInline(body.Old, body.New, body.Mode)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Diff handles POST /schemas/diff.
func (h *Handler) Diff(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Old json.RawMessage `json:"old"`
		New json.RawMessage `json:"new"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON", "", 0)
		return
	}
	res, err := h.r.DiffSchemas(body.Old, body.New)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GetConfig handles GET /config/{subject}.
func (h *Handler) GetConfig(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	mode, err := h.r.GetConfig(subject)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"subject": subject, "compatibility": mode})
}

// SetConfig handles PUT /config/{subject}.
func (h *Handler) SetConfig(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	var body struct {
		Compatibility string `json:"compatibility"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON", "", 0)
		return
	}
	if err := h.r.SetConfig(subject, body.Compatibility); err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"subject": subject, "compatibility": body.Compatibility})
}

// GetGlobalConfig handles GET /config.
func (h *Handler) GetGlobalConfig(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"default_compatibility": h.r.GetGlobalConfig()})
}

// SetGlobalConfig handles PUT /config.
func (h *Handler) SetGlobalConfig(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Compatibility string `json:"compatibility"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON", "", 0)
		return
	}
	if err := h.r.SetGlobalConfig(body.Compatibility); err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"default_compatibility": body.Compatibility})
}

// Stats handles GET /subjects/{subject}/stats.
func (h *Handler) Stats(w http.ResponseWriter, req *http.Request) {
	stats, err := h.r.Stats(req.PathValue("subject"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// Audit handles GET /subjects/{subject}/audit?limit=N.
func (h *Handler) Audit(w http.ResponseWriter, req *http.Request) {
	subject := req.PathValue("subject")
	limit := 0
	fmtScan(req.URL.Query().Get("limit"), &limit)
	rows, err := h.r.Audit(subject, limit)
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"subject": subject, "audit": rows})
}

// Export handles GET /subjects/{subject}/export.
func (h *Handler) Export(w http.ResponseWriter, req *http.Request) {
	exp, err := h.r.ExportSubject(req.PathValue("subject"))
	if err != nil {
		status, code := classifyErr(err)
		writeErr(w, status, code, err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, exp)
}

// fmtScan parses an integer from a string without importing fmt at the top
// level (kept here for the audit limit query param).
func fmtScan(s string, dst *int) {
	if s == "" {
		return
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return
		}
		n = n*10 + int(c-'0')
	}
	*dst = n
}
