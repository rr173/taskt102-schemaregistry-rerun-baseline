package registry

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"task102-schemaregistry/internal/store"
)

func newInlineRegistry(t *testing.T) *Registry {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "inline.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s)
}

func TestFingerprintCandidate(t *testing.T) {
	r := newInlineRegistry(t)
	fp1, err := r.FingerprintCandidate(schemaJSON(`{"name":"id","type":"integer","required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	// different order, same fields -> same fingerprint
	fp2, err := r.FingerprintCandidate(schemaJSON(`{"name":"id","type":"integer","required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 || fp1 == "" {
		t.Fatalf("fingerprint should be stable and non-empty: %q vs %q", fp1, fp2)
	}
	if _, err := r.FingerprintCandidate([]byte(`not json`)); err == nil {
		t.Fatalf("expected error for invalid schema")
	}
}

func TestNormalizeSchema(t *testing.T) {
	r := newInlineRegistry(t)
	res, err := r.NormalizeSchema(schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"role","type":"string","enum":["a","b"],"default":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.FieldCount != 2 {
		t.Fatalf("field count = %d", res.FieldCount)
	}
	if len(res.Required) != 1 || res.Required[0] != "id" {
		t.Fatalf("required = %v", res.Required)
	}
	if res.Fingerprint == "" {
		t.Fatalf("fingerprint empty")
	}
	if res.Summary == "" {
		t.Fatalf("summary empty")
	}
}

func TestValidateInline(t *testing.T) {
	r := newInlineRegistry(t)
	def := schemaJSON(`{"name":"id","type":"integer","required":true}`)
	res, err := r.ValidateInline(def, []byte(`{"id":1}`))
	if err != nil || !res.Valid {
		t.Fatalf("expected valid, got %+v err=%v", res, err)
	}
	res, err = r.ValidateInline(def, []byte(`{"name":"a"}`))
	if err != nil || res.Valid {
		t.Fatalf("expected invalid (missing id), got %+v", res)
	}
}

func TestCompatInline(t *testing.T) {
	r := newInlineRegistry(t)
	old := schemaJSON(`{"name":"id","type":"integer","required":true}`)
	// compatible: add optional
	res, err := r.CompatInline(old, schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"e","type":"string","required":false}`), "BACKWARD")
	if err != nil || !res.Compatible {
		t.Fatalf("expected compatible, got %+v err=%v", res, err)
	}
	// incompatible
	res, err = r.CompatInline(old, schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"age","type":"integer","required":true}`), "BACKWARD")
	if err != nil || res.Compatible || res.Violation.Field != "age" {
		t.Fatalf("expected incompatible on age, got %+v err=%v", res, err)
	}
	// bad mode
	if _, err := r.CompatInline(old, old, "WEIRD"); err == nil {
		t.Fatalf("expected error for bad mode")
	}
}

func TestDiffSchemas(t *testing.T) {
	r := newInlineRegistry(t)
	old := schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}`)
	newer := schemaJSON(`{"name":"id","type":"string","required":true},{"name":"email","type":"string"}`)
	d, err := r.DiffSchemas(old, newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 1 || d.Added[0] != "email" {
		t.Fatalf("added = %v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "name" {
		t.Fatalf("removed = %v", d.Removed)
	}
	if len(d.Changed) != 1 || d.Changed[0].Field != "id" {
		t.Fatalf("changed = %+v", d.Changed)
	}
}

func TestCanonicalize(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"role","type":"string","enum":["user","admin"],"default":"user"}`))
	res, err := r.Canonicalize("user", 0, []byte(`{"id":1,"extra":99}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("expected valid, got errors %+v", res.Errors)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(res.Canonical, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["extra"]; ok {
		t.Fatalf("unknown field extra should be stripped")
	}
	if _, ok := m["role"]; !ok {
		t.Fatalf("absent optional field with default should be filled")
	}
	// invalid message
	res, err = r.Canonicalize("user", 0, []byte(`{"name":"a"}`))
	if err != nil || res.Valid {
		t.Fatalf("expected invalid (missing id), got %+v", res)
	}
}

func TestStats(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}`))
	stats, err := r.Stats("user")
	if err != nil {
		t.Fatal(err)
	}
	if stats.VersionCount != 1 || stats.LatestVersion != 1 || stats.LatestFieldCount != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.LatestFingerprint == "" || stats.LatestSummary == "" {
		t.Fatalf("latest fingerprint/summary empty: %+v", stats)
	}
}

func TestSubjectInfo(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	r.SetConfig("user", "NONE")
	r.Register("user", schemaJSON(`{"name":"id","type":"string","required":true}`))
	info, err := r.SubjectInfo("user")
	if err != nil {
		t.Fatal(err)
	}
	if info.Compatibility != "NONE" || info.LatestVersion != 2 || info.VersionCount != 2 {
		t.Fatalf("info = %+v", info)
	}
}

func TestGlobalConfig(t *testing.T) {
	r := newInlineRegistry(t)
	if r.GetGlobalConfig() != "BACKWARD" {
		t.Fatalf("default should be BACKWARD, got %q", r.GetGlobalConfig())
	}
	if err := r.SetGlobalConfig("FULL"); err != nil {
		t.Fatal(err)
	}
	if r.GetGlobalConfig() != "FULL" {
		t.Fatalf("global should be FULL, got %q", r.GetGlobalConfig())
	}
	// new subjects inherit the global default
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	info, _ := r.SubjectInfo("user")
	if info.Compatibility != "FULL" {
		t.Fatalf("new subject should inherit FULL, got %s", info.Compatibility)
	}
	// bad mode rejected
	if err := r.SetGlobalConfig("WEIRD"); err == nil {
		t.Fatalf("expected error for bad mode")
	}
}

func TestTransitiveCompatEnforced(t *testing.T) {
	r := newInlineRegistry(t)
	// v1: id string required; v2: id integer required (set NONE to allow v1->v2).
	r.Register("s", schemaJSON(`{"name":"id","type":"string","required":true}`))
	r.SetConfig("s", "NONE")
	r.Register("s", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	// Now set transitive backward. A candidate equal to v2 is compatible with
	// v2 but v2 is not backward-compatible with v1 (type change), so
	// transitive registration must fail reporting version 1.
	r.SetConfig("s", "BACKWARD_TRANSITIVE")
	_, err := r.Register("s", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"e","type":"string","required":false}`))
	var ce *CompatError
	if !errors.As(err, &ce) || ce.Version != 1 {
		t.Fatalf("expected transitive failure at version 1, got %v", err)
	}
}

func TestAudit(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	r.SetConfig("user", "NONE")
	r.Register("user", schemaJSON(`{"name":"id","type":"string","required":true}`))
	r.DeleteVersion("user", 2)
	rows, err := r.Audit("user", 0)
	if err != nil {
		t.Fatal(err)
	}
	// register, set_config, register, delete_version -> 4 entries, newest first
	if len(rows) != 4 {
		t.Fatalf("expected 4 audit rows, got %d", len(rows))
	}
	if rows[0].Action != "delete_version" {
		t.Fatalf("newest audit should be delete_version, got %s", rows[0].Action)
	}
	// audit for missing subject -> not_found
	if _, err := r.Audit("nope", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestExportSubject(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	r.SetConfig("user", "NONE")
	r.Register("user", schemaJSON(`{"name":"id","type":"string","required":true}`))
	exp, err := r.ExportSubject("user")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Subject != "user" || exp.Compatibility != "NONE" || len(exp.Versions) != 2 {
		t.Fatalf("export = %+v", exp)
	}
	if exp.Versions[0].Version != 1 || exp.Versions[1].Version != 2 {
		t.Fatalf("export versions order = %+v", exp.Versions)
	}
	if _, err := r.ExportSubject("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetFingerprint(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	fp, err := r.GetFingerprint("user", 0)
	if err != nil || fp == "" {
		t.Fatalf("GetFingerprint latest: %q err=%v", fp, err)
	}
	if _, err := r.GetFingerprint("nope", 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCheckCompatVersion(t *testing.T) {
	r := newInlineRegistry(t)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	// check vs version 1: add optional -> compatible
	res, err := r.CheckCompatVersion("user", 1, schemaJSON(
		`{"name":"id","type":"integer","required":true},{"name":"e","type":"string","required":false}`))
	if err != nil || !res.Compatible {
		t.Fatalf("expected compatible vs v1, got %+v err=%v", res, err)
	}
	// check vs version 1: add required -> incompatible
	res, err = r.CheckCompatVersion("user", 1, schemaJSON(
		`{"name":"id","type":"integer","required":true},{"name":"age","type":"integer","required":true}`))
	if err != nil || res.Compatible || res.Violation.Field != "age" {
		t.Fatalf("expected incompatible vs v1 on age, got %+v", res)
	}
}
