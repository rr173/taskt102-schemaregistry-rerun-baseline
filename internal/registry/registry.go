// Package registry orchestrates schema registration, retrieval, deletion,
// compatibility configuration, message validation, schema diffing, audit
// logging and export on top of the store. It is the single place that composes
// parsing, compatibility checking and the atomic store operations, and it
// serializes mutations with a mutex so that a registration's read-check-write
// sequence is consistent.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"task102-schemaregistry/internal/compat"
	"task102-schemaregistry/internal/diff"
	"task102-schemaregistry/internal/schema"
	"task102-schemaregistry/internal/store"
)

// ErrNotFound is returned when a subject or version does not exist.
var ErrNotFound = store.ErrNotFound

// metaKeyDefaultCompat is the meta key for the global default compatibility.
const metaKeyDefaultCompat = "default_compatibility"

// ErrNotLatest is returned when deleting a version that is not the latest.
type ErrNotLatest struct {
	Latest int
}

func (e *ErrNotLatest) Error() string {
	return fmt.Sprintf("只能删除最新版本（当前最新版本号为 %d）", e.Latest)
}

// collapseError adds context while preserving the original typed error for
// callers that classify errors through errors.As or errors.Is.
func collapseError(err error) error {
	return fmt.Errorf("registry operation failed: %v", err)
}

// CompatError reports that a candidate schema failed a compatibility check.
type CompatError struct {
	Field   string `json:"field,omitempty"`
	Reason  string `json:"reason"`
	Version int    `json:"version,omitempty"`
}

func (e *CompatError) Error() string {
	if e.Field == "" {
		if e.Version > 0 {
			return fmt.Sprintf("版本 %d: %s", e.Version, e.Reason)
		}
		return e.Reason
	}
	if e.Version > 0 {
		return fmt.Sprintf("版本 %d 字段 %s: %s", e.Version, e.Field, e.Reason)
	}
	return fmt.Sprintf("字段 %s: %s", e.Field, e.Reason)
}

// RegisterResult is returned by Register.
type RegisterResult struct {
	Subject     string `json:"subject"`
	Version     int    `json:"version"`
	Fingerprint string `json:"fingerprint"`
	Idempotent  bool   `json:"idempotent"`
}

// SubjectInfo is the public view of a subject's metadata.
type SubjectInfo struct {
	Name          string `json:"name"`
	Compatibility string `json:"compatibility"`
	LatestVersion int    `json:"latest_version"`
	VersionCount  int    `json:"version_count"`
	CreatedAt     string `json:"created_at"`
}

// SubjectStats extends SubjectInfo with a summary of the latest version.
type SubjectStats struct {
	Subject           string `json:"subject"`
	Compatibility     string `json:"compatibility"`
	LatestVersion     int    `json:"latest_version"`
	VersionCount      int    `json:"version_count"`
	LatestFingerprint string `json:"latest_fingerprint,omitempty"`
	LatestFieldCount  int    `json:"latest_field_count,omitempty"`
	LatestSummary     string `json:"latest_summary,omitempty"`
}

// SchemaView is the public view of a schema version.
type SchemaView struct {
	Subject     string          `json:"subject"`
	Version     int             `json:"version"`
	Definition  json.RawMessage `json:"definition"`
	Fingerprint string          `json:"fingerprint"`
	CreatedAt   string          `json:"created_at,omitempty"`
}

// ValidationResult is returned by Validate and ValidateInline.
type ValidationResult struct {
	Valid  bool                     `json:"valid"`
	Errors []schema.ValidationError `json:"errors,omitempty"`
}

// CanonicalizeResult is returned by Canonicalize.
type CanonicalizeResult struct {
	Canonical json.RawMessage          `json:"canonical"`
	Valid     bool                     `json:"valid"`
	Errors    []schema.ValidationError `json:"errors,omitempty"`
}

// CompatResult is returned by CheckCompat and CompatInline.
type CompatResult struct {
	Compatible bool              `json:"compatible"`
	Violation  *compat.Violation `json:"violation,omitempty"`
}

// NormalizeResult is returned by NormalizeSchema.
type NormalizeResult struct {
	Definition  json.RawMessage `json:"definition"`
	Fingerprint string          `json:"fingerprint"`
	FieldCount  int             `json:"field_count"`
	Required    []string        `json:"required,omitempty"`
	Summary     string          `json:"summary"`
}

// ExportResult is a subject's full, recoverable history.
type ExportResult struct {
	Subject       string       `json:"subject"`
	Compatibility string       `json:"compatibility"`
	Versions      []SchemaView `json:"versions"`
}

// Registry composes a store with schema/compat/diff logic.
type Registry struct {
	store *store.Store
	mu    sync.Mutex
}

// New creates a Registry backed by the given store.
func New(s *store.Store) *Registry {
	return &Registry{store: s}
}

// nowStamp returns a stable UTC timestamp string.
func nowStamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// defaultCompat returns the global default compatibility mode (BACKWARD when
// unset or invalid).
func (r *Registry) defaultCompat() string {
	v, err := r.store.GetMeta(metaKeyDefaultCompat)
	if err != nil || v == "" {
		return string(compat.Backward)
	}
	if _, err := compat.ParseMode(v); err != nil {
		return string(compat.Backward)
	}
	return v
}

// Register parses, compatibility-checks and persists a new schema version.
func (r *Registry) Register(subject string, def []byte) (*RegisterResult, error) {
	cand, err := schema.Parse(def)
	if err != nil {
		return nil, fmt.Errorf("契约定义不合法: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	compatMode := r.defaultCompat()
	if err := r.store.EnsureSubject(subject, compatMode, nowStamp()); err != nil {
		return nil, err
	}
	subj, err := r.store.GetSubject(subject)
	if err != nil {
		return nil, collapseError(err)
	}
	mode, _ := compat.ParseMode(subj.Compatibility)

	latest, err := r.store.LatestSchema(subject)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if latest != nil {
		// Idempotent: identical to the latest version -> return it.
		if latest.Fingerprint == cand.Fingerprint() {
			return &RegisterResult{Subject: subject, Version: latest.Version, Fingerprint: latest.Fingerprint, Idempotent: true}, nil
		}
		// Compatibility check.
		olds, err := r.priorVersions(subject, mode)
		if err != nil {
			return nil, err
		}
		if ok, v := compat.CheckAgainst(mode, olds, cand); !ok {
			return nil, &CompatError{Field: v.Field, Reason: v.Reason, Version: v.Version}
		}
	}
	defJSON, err := cand.DefinitionJSON()
	if err != nil {
		return nil, err
	}
	version, err := r.store.RegisterVersion(subject, subj.Compatibility, string(defJSON), cand.Fingerprint(), nowStamp())
	if err != nil {
		return nil, err
	}
	_ = r.store.AppendAudit(subject, "register", version, cand.Fingerprint(), nowStamp())
	return &RegisterResult{Subject: subject, Version: version, Fingerprint: cand.Fingerprint()}, nil
}

// priorVersions returns the list of prior schemas needed for a compatibility
// check. For transitive modes every prior version is returned; otherwise only
// the latest version is returned (as a one-element list).
func (r *Registry) priorVersions(subject string, mode compat.Mode) ([]compat.VersionedSchema, error) {
	if !compat.IsTransitive(mode) {
		latest, err := r.store.LatestSchema(subject)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		s, err := schema.Parse([]byte(latest.Definition))
		if err != nil {
			return nil, fmt.Errorf("内部错误: 解析已存契约失败: %w", err)
		}
		return []compat.VersionedSchema{{Version: latest.Version, Schema: s}}, nil
	}
	rows, err := r.store.AllSchemas(subject)
	if err != nil {
		return nil, err
	}
	out := make([]compat.VersionedSchema, 0, len(rows))
	for _, row := range rows {
		s, err := schema.Parse([]byte(row.Definition))
		if err != nil {
			return nil, fmt.Errorf("内部错误: 解析版本 %d 失败: %w", row.Version, err)
		}
		out = append(out, compat.VersionedSchema{Version: row.Version, Schema: s})
	}
	return out, nil
}

// GetSchema returns a schema version (version=0 means latest).
func (r *Registry) GetSchema(subject string, version int) (*SchemaView, error) {
	row, err := r.getSchemaRow(subject, version)
	if err != nil {
		return nil, err
	}
	return &SchemaView{Subject: row.Subject, Version: row.Version, Definition: json.RawMessage(row.Definition), Fingerprint: row.Fingerprint, CreatedAt: row.CreatedAt}, nil
}

func (r *Registry) getSchemaRow(subject string, version int) (*store.SchemaRow, error) {
	if version == 0 {
		return r.store.LatestSchema(subject)
	}
	return r.store.GetSchema(subject, version)
}

// ListVersions returns a subject's version numbers.
func (r *Registry) ListVersions(subject string) ([]int, error) {
	if _, err := r.store.GetSubject(subject); err != nil {
		return nil, err
	}
	return r.store.ListVersions(subject)
}

// ListSubjects returns all subjects with their latest version and mode.
func (r *Registry) ListSubjects() ([]SubjectInfo, error) {
	rows, err := r.store.ListSubjects()
	if err != nil {
		return nil, err
	}
	var out []SubjectInfo
	for _, sr := range rows {
		latest, _ := r.store.LatestVersion(sr.Name)
		out = append(out, SubjectInfo{
			Name: sr.Name, Compatibility: sr.Compatibility,
			LatestVersion: latest, VersionCount: latestCount(sr.Name, r),
			CreatedAt: sr.CreatedAt,
		})
	}
	return out, nil
}

// latestCount is a small helper that returns the number of versions of a
// subject, tolerating errors as zero.
func latestCount(name string, r *Registry) int {
	vs, err := r.store.ListVersions(name)
	if err != nil {
		return 0
	}
	return len(vs)
}

// DeleteSubject removes a subject and all its versions.
func (r *Registry) DeleteSubject(subject string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.store.GetSubject(subject); err != nil {
		return err
	}
	if err := r.store.DeleteSubject(subject); err != nil {
		return err
	}
	_ = r.store.AppendAudit(subject, "delete_subject", 0, "", nowStamp())
	return nil
}

// DeleteVersion removes the latest version of a subject. Deleting a non-latest
// version returns an *ErrNotLatest carrying the current latest version.
func (r *Registry) DeleteVersion(subject string, version int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	latest, err := r.store.LatestVersion(subject)
	if err != nil {
		return err
	}
	if latest == 0 {
		return store.ErrNotFound
	}
	if version != latest {
		return &ErrNotLatest{Latest: latest}
	}
	if err := r.store.DeleteVersion(subject, version); err != nil {
		return err
	}
	_ = r.store.AppendAudit(subject, "delete_version", version, "", nowStamp())
	return nil
}

// CheckCompat checks a candidate against the latest version without persisting.
func (r *Registry) CheckCompat(subject string, def []byte) (*CompatResult, error) {
	cand, err := schema.Parse(def)
	if err != nil {
		return nil, fmt.Errorf("契约定义不合法: %w", err)
	}
	subj, err := r.store.GetSubject(subject)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &CompatResult{Compatible: true}, nil
		}
		return nil, err
	}
	mode, _ := compat.ParseMode(subj.Compatibility)
	olds, err := r.priorVersions(subject, mode)
	if err != nil {
		return nil, err
	}
	if len(olds) > 0 {
		olds = olds[len(olds)-1:]
	}
	if ok, v := compat.CheckAgainst(mode, olds, cand); !ok {
		vv := v
		return &CompatResult{Compatible: false, Violation: &vv}, nil
	}
	return &CompatResult{Compatible: true}, nil
}

// CheckCompatVersion checks a candidate against a specific prior version.
func (r *Registry) CheckCompatVersion(subject string, version int, def []byte) (*CompatResult, error) {
	cand, err := schema.Parse(def)
	if err != nil {
		return nil, fmt.Errorf("契约定义不合法: %w", err)
	}
	row, err := r.store.GetSchema(subject, version)
	if err != nil {
		return nil, collapseError(err)
	}
	subj, err := r.store.GetSubject(subject)
	if err != nil {
		return nil, collapseError(err)
	}
	mode, _ := compat.ParseMode(subj.Compatibility)
	old, err := schema.Parse([]byte(row.Definition))
	if err != nil {
		return nil, fmt.Errorf("内部错误: 解析已存契约失败: %w", err)
	}
	if ok, v := compat.CheckAgainst(mode, []compat.VersionedSchema{{Version: version, Schema: old}}, cand); !ok {
		vv := v
		return &CompatResult{Compatible: false, Violation: &vv}, nil
	}
	return &CompatResult{Compatible: true}, nil
}

// Validate checks a message against a schema version (version=0 means latest).
func (r *Registry) Validate(subject string, version int, msg []byte) (*ValidationResult, error) {
	row, err := r.getSchemaRow(subject, version)
	if err != nil {
		return nil, err
	}
	s, err := schema.Parse([]byte(row.Definition))
	if err != nil {
		return nil, fmt.Errorf("内部错误: 解析契约失败: %w", err)
	}
	errs := s.Validate(msg)
	return &ValidationResult{Valid: len(errs) == 0, Errors: errs}, nil
}

// Canonicalize validates a message and returns a schema-conforming copy with
// defaults filled and unknown fields removed.
func (r *Registry) Canonicalize(subject string, version int, msg []byte) (*CanonicalizeResult, error) {
	row, err := r.getSchemaRow(subject, version)
	if err != nil {
		return nil, err
	}
	s, err := schema.Parse([]byte(row.Definition))
	if err != nil {
		return nil, fmt.Errorf("内部错误: 解析契约失败: %w", err)
	}
	canon, errs := s.Canonicalize(msg)
	if len(errs) > 0 {
		return &CanonicalizeResult{Valid: false, Errors: errs}, nil
	}
	return &CanonicalizeResult{Canonical: canon, Valid: true}, nil
}

// GetConfig returns a subject's compatibility mode.
func (r *Registry) GetConfig(subject string) (string, error) {
	subj, err := r.store.GetSubject(subject)
	if err != nil {
		return "", err
	}
	return subj.Compatibility, nil
}

// SetConfig ensures the subject exists and sets its compatibility mode.
func (r *Registry) SetConfig(subject string, mode string) error {
	if _, err := compat.ParseMode(mode); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.SetCompatibility(subject, mode, nowStamp()); err != nil {
		return err
	}
	_ = r.store.AppendAudit(subject, "set_config", 0, mode, nowStamp())
	return nil
}

// GetGlobalConfig returns the default compatibility mode for new subjects.
func (r *Registry) GetGlobalConfig() string {
	return r.defaultCompat()
}

// SetGlobalConfig sets the default compatibility mode for new subjects.
func (r *Registry) SetGlobalConfig(mode string) error {
	if _, err := compat.ParseMode(mode); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.SetMeta(metaKeyDefaultCompat, mode)
}

// SubjectInfo returns a subject's metadata.
func (r *Registry) SubjectInfo(subject string) (*SubjectInfo, error) {
	subj, err := r.store.GetSubject(subject)
	if err != nil {
		return nil, err
	}
	versions, err := r.store.ListVersions(subject)
	if err != nil {
		return nil, err
	}
	latest := 0
	if len(versions) > 0 {
		latest = versions[len(versions)-1]
	}
	return &SubjectInfo{
		Name: subj.Name, Compatibility: subj.Compatibility,
		LatestVersion: latest, VersionCount: len(versions),
		CreatedAt: subj.CreatedAt,
	}, nil
}

// Stats returns a subject's metadata plus a summary of its latest version.
func (r *Registry) Stats(subject string) (*SubjectStats, error) {
	info, err := r.SubjectInfo(subject)
	if err != nil {
		return nil, err
	}
	stats := &SubjectStats{
		Subject: info.Name, Compatibility: info.Compatibility,
		LatestVersion: info.LatestVersion, VersionCount: info.VersionCount,
	}
	if info.LatestVersion > 0 {
		if row, err := r.store.GetSchema(subject, info.LatestVersion); err == nil {
			stats.LatestFingerprint = row.Fingerprint
			if s, err := schema.Parse([]byte(row.Definition)); err == nil {
				stats.LatestFieldCount = s.FieldCount()
				stats.LatestSummary = s.Summary()
			}
		}
	}
	return stats, nil
}

// FingerprintCandidate parses an inline schema and returns its fingerprint.
func (r *Registry) FingerprintCandidate(def []byte) (string, error) {
	s, err := schema.Parse(def)
	if err != nil {
		return "", fmt.Errorf("契约定义不合法: %w", err)
	}
	return s.Fingerprint(), nil
}

// NormalizeSchema parses an inline schema and returns its canonical
// definition, fingerprint and a summary.
func (r *Registry) NormalizeSchema(def []byte) (*NormalizeResult, error) {
	s, err := schema.Parse(def)
	if err != nil {
		return nil, fmt.Errorf("契约定义不合法: %w", err)
	}
	dj, err := s.DefinitionJSON()
	if err != nil {
		return nil, err
	}
	return &NormalizeResult{
		Definition:  json.RawMessage(dj),
		Fingerprint: s.Fingerprint(),
		FieldCount:  s.FieldCount(),
		Required:    s.RequiredFields(),
		Summary:     s.Summary(),
	}, nil
}

// ValidateInline validates a message against an inline schema.
func (r *Registry) ValidateInline(def, msg []byte) (*ValidationResult, error) {
	s, err := schema.Parse(def)
	if err != nil {
		return nil, fmt.Errorf("契约定义不合法: %w", err)
	}
	errs := s.Validate(msg)
	return &ValidationResult{Valid: len(errs) == 0, Errors: errs}, nil
}

// CompatInline checks compatibility between two inline schemas under a mode.
func (r *Registry) CompatInline(oldDef, newDef []byte, modeStr string) (*CompatResult, error) {
	old, err := schema.Parse(oldDef)
	if err != nil {
		return nil, fmt.Errorf("旧契约不合法: %w", err)
	}
	newer, err := schema.Parse(newDef)
	if err != nil {
		return nil, fmt.Errorf("新契约不合法: %w", err)
	}
	mode, err := compat.ParseMode(modeStr)
	if err != nil {
		return nil, err
	}
	if ok, v := compat.Check(mode, old, newer); !ok {
		vv := v
		return &CompatResult{Compatible: false, Violation: &vv}, nil
	}
	return &CompatResult{Compatible: true}, nil
}

// DiffSchemas computes the field-level difference between two inline schemas.
func (r *Registry) DiffSchemas(oldDef, newDef []byte) (*diff.Result, error) {
	old, err := schema.Parse(oldDef)
	if err != nil {
		return nil, fmt.Errorf("旧契约不合法: %w", err)
	}
	newer, err := schema.Parse(newDef)
	if err != nil {
		return nil, fmt.Errorf("新契约不合法: %w", err)
	}
	return diff.Compute(old, newer), nil
}

// GetFingerprint returns the fingerprint of a stored version (0=latest).
func (r *Registry) GetFingerprint(subject string, version int) (string, error) {
	row, err := r.getSchemaRow(subject, version)
	if err != nil {
		return "", err
	}
	return row.Fingerprint, nil
}

// Audit returns the audit log for a subject (newest first).
func (r *Registry) Audit(subject string, limit int) ([]store.AuditRow, error) {
	if _, err := r.store.GetSubject(subject); err != nil {
		return nil, err
	}
	return r.store.ListAudit(subject, limit)
}

// ExportSubject returns a subject's full history (meta + all versions).
func (r *Registry) ExportSubject(subject string) (*ExportResult, error) {
	subj, err := r.store.GetSubject(subject)
	if err != nil {
		return nil, err
	}
	rows, err := r.store.AllSchemas(subject)
	if err != nil {
		return nil, err
	}
	versions := make([]SchemaView, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, SchemaView{
			Subject: row.Subject, Version: row.Version,
			Definition: json.RawMessage(row.Definition), Fingerprint: row.Fingerprint,
			CreatedAt: row.CreatedAt,
		})
	}
	return &ExportResult{Subject: subj.Name, Compatibility: subj.Compatibility, Versions: versions}, nil
}

// LookupVersion resolves "latest" to the latest version number, or parses an
// integer. It returns ErrNotFound if the subject has no versions.
func (r *Registry) LookupVersion(subject, v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "latest" {
		latest, err := r.store.LatestVersion(subject)
		if err != nil {
			return 0, err
		}
		if latest == 0 {
			return 0, store.ErrNotFound
		}
		return latest, nil
	}
	var n int
	_, err := fmt.Sscanf(v, "%d", &n)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("非法版本号 %q", v)
	}
	return n, nil
}
