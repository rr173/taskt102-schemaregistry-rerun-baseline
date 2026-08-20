package registry

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"task102-schemaregistry/internal/store"
)

func newRegistry(t *testing.T, persist bool) (*Registry, *store.Store) {
	t.Helper()
	var path string
	if persist {
		path = filepath.Join(t.TempDir(), "registry.db")
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s), s
}

func schemaJSON(fields string) []byte {
	return []byte(`{"fields":[` + fields + `]}`)
}

func TestRegisterFirstVersion(t *testing.T) {
	r, _ := newRegistry(t, false)
	res, err := r.Register("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true}`))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if res.Version != 1 {
		t.Fatalf("expected version 1, got %d", res.Version)
	}
	view, err := r.GetSchema("user", 1)
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	if view.Version != 1 {
		t.Fatalf("view version = %d", view.Version)
	}
	// fields present
	var got struct {
		Fields []map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(view.Definition, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(got.Fields))
	}
}

func TestRegisterEvolveBackwardOK(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}`))
	res, err := r.Register("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true},
		 {"name":"email","type":"string","required":false,"default":""}`))
	if err != nil {
		t.Fatalf("Register evolve: %v", err)
	}
	if res.Version != 2 {
		t.Fatalf("expected version 2, got %d", res.Version)
	}
	vs, _ := r.ListVersions("user")
	if len(vs) != 2 || vs[0] != 1 || vs[1] != 2 {
		t.Fatalf("versions = %v", vs)
	}
}

func TestRegisterBackwardIncompatibleRejected(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}`))
	_, err := r.Register("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true},
		 {"name":"age","type":"integer","required":true}`))
	var ce *CompatError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CompatError, got %T %v", err, err)
	}
	if ce.Field != "age" {
		t.Fatalf("expected violation on age, got %s", ce.Field)
	}
	// version did not advance
	vs, _ := r.ListVersions("user")
	if len(vs) != 1 || vs[0] != 1 {
		t.Fatalf("version should not advance after compat failure: %v", vs)
	}
}

func TestRegisterIdempotent(t *testing.T) {
	r, _ := newRegistry(t, false)
	v2def := schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true},
		 {"name":"email","type":"string","required":false,"default":""}`)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}`))
	if _, err := r.Register("user", v2def); err != nil {
		t.Fatal(err)
	}
	// resubmit with different field order
	reordered := schemaJSON(
		`{"name":"email","type":"string","required":false,"default":""},
		 {"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true}`)
	res, err := r.Register("user", reordered)
	if err != nil {
		t.Fatalf("idempotent Register: %v", err)
	}
	if !res.Idempotent || res.Version != 2 {
		t.Fatalf("expected idempotent v2, got %+v", res)
	}
	vs, _ := r.ListVersions("user")
	if len(vs) != 2 {
		t.Fatalf("no new version after idempotent: %v", vs)
	}
}

func TestValidate(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true},
		 {"name":"role","type":"string","enum":["user","admin"],"default":"user"}`))
	cases := []struct {
		name    string
		msg     string
		wantErr bool
		field   string
	}{
		{"valid", `{"id":1,"name":"a","role":"user"}`, false, ""},
		{"unknown-ignored", `{"id":1,"name":"a","extra":1}`, false, ""},
		{"missing-required", `{"name":"a"}`, true, "id"},
		{"type-mismatch", `{"id":"1","name":"a"}`, true, "id"},
		{"enum-oob", `{"id":1,"name":"a","role":"superuser"}`, true, "role"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := r.Validate("user", 1, []byte(c.msg))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if c.wantErr {
				if res.Valid {
					t.Fatalf("expected invalid, got valid")
				}
				if c.field != "" && (len(res.Errors) == 0 || res.Errors[0].Field != c.field) {
					t.Fatalf("expected error on %s, got %+v", c.field, res.Errors)
				}
			} else if !res.Valid {
				t.Fatalf("expected valid, got errors %+v", res.Errors)
			}
		})
	}
}

func TestValidateLatest(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	res, err := r.Validate("user", 0, []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("Validate latest: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid, got %+v", res.Errors)
	}
}

func TestValidateNotFound(t *testing.T) {
	r, _ := newRegistry(t, false)
	if _, err := r.Validate("nope", 1, []byte(`{}`)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCheckCompat(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	// compatible: add optional
	res, err := r.CheckCompat("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"e","type":"string","required":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compatible {
		t.Fatalf("expected compatible, got %+v", res.Violation)
	}
	// incompatible: add required no default
	res, err = r.CheckCompat("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"age","type":"integer","required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Compatible || res.Violation.Field != "age" {
		t.Fatalf("expected incompatible on age, got %+v", res.Violation)
	}
	// no new version persisted
	vs, _ := r.ListVersions("user")
	if len(vs) != 1 {
		t.Fatalf("CheckCompat must not persist: %v", vs)
	}
}

func TestCheckCompatNoPrior(t *testing.T) {
	r, _ := newRegistry(t, false)
	res, err := r.CheckCompat("newsubj", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compatible {
		t.Fatalf("no prior version should be compatible")
	}
}

func TestSetConfigNoneAllowsIncompatible(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	if err := r.SetConfig("user", "NONE"); err != nil {
		t.Fatal(err)
	}
	_, err := r.Register("user", schemaJSON(`{"name":"id","type":"string","required":true}`))
	if err != nil {
		t.Fatalf("NONE mode should allow incompatible, got %v", err)
	}
}

func TestSetConfigCreatesSubject(t *testing.T) {
	r, _ := newRegistry(t, false)
	if err := r.SetConfig("cfg", "FORWARD"); err != nil {
		t.Fatal(err)
	}
	cfg, err := r.GetConfig("cfg")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != "FORWARD" {
		t.Fatalf("expected FORWARD, got %s", cfg)
	}
}

func TestSetConfigBadMode(t *testing.T) {
	r, _ := newRegistry(t, false)
	if err := r.SetConfig("cfg", "WEIRD"); err == nil {
		t.Fatalf("expected error for bad mode")
	}
}

func TestForwardModeDeleteRequiredFails(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("cfg", schemaJSON(`{"name":"k","type":"string","required":true}`))
	r.SetConfig("cfg", "FORWARD")
	_, err := r.Register("cfg", schemaJSON(""))
	var ce *CompatError
	if !errors.As(err, &ce) || ce.Field != "k" {
		t.Fatalf("expected forward fail on k, got %v", err)
	}
}

func TestDeleteVersionNotLatest(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	r.SetConfig("user", "NONE")
	r.Register("user", schemaJSON(`{"name":"id","type":"string","required":true}`))
	err := r.DeleteVersion("user", 1)
	var e *ErrNotLatest
	if !errors.As(err, &e) {
		t.Fatalf("expected ErrNotLatest, got %v", err)
	}
	if e.Latest != 2 {
		t.Fatalf("expected latest=2, got %d", e.Latest)
	}
}

func TestDeleteVersionLatest(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	r.SetConfig("user", "NONE")
	r.Register("user", schemaJSON(`{"name":"id","type":"string","required":true}`))
	if err := r.DeleteVersion("user", 2); err != nil {
		t.Fatalf("DeleteVersion latest: %v", err)
	}
	latest, _ := r.ListVersions("user")
	if len(latest) != 1 || latest[0] != 1 {
		t.Fatalf("after deleting v2, should have [1], got %v", latest)
	}
	// version not reused
	res, err := r.Register("user", schemaJSON(`{"name":"id","type":"number","required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != 3 {
		t.Fatalf("version should be 3 (no reuse), got %d", res.Version)
	}
}

func TestDeleteSubject(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	if err := r.DeleteSubject("user"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteSubject("user"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListSubjects(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("zeta", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	r.Register("alpha", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	subs, err := r.ListSubjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs[0].Name != "alpha" || subs[1].Name != "zeta" {
		t.Fatalf("expected [alpha, zeta], got %+v", subs)
	}
}

func TestRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	s1, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r1 := New(s1)
	r1.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}`))
	r1.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true},{"name":"email","type":"string","required":false,"default":""}`))
	r1.SetConfig("user", "FULL")
	s1.Close()

	s2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s2.Close() })
	r2 := New(s2)
	subs, err := r2.ListSubjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Name != "user" || subs[0].Compatibility != "FULL" || subs[0].LatestVersion != 2 {
		t.Fatalf("recovery mismatch: %+v", subs)
	}
	vs, _ := r2.ListVersions("user")
	if len(vs) != 2 {
		t.Fatalf("versions not recovered: %v", vs)
	}
	// idempotent still works after restart
	res, err := r2.Register("user", schemaJSON(
		`{"name":"id","type":"integer","required":true},
		 {"name":"name","type":"string","required":true},
		 {"name":"email","type":"string","required":false,"default":""}`))
	if err != nil {
		t.Fatalf("idempotent after restart: %v", err)
	}
	if !res.Idempotent || res.Version != 2 {
		t.Fatalf("expected idempotent v2 after restart, got %+v", res)
	}
}

func TestLookupVersion(t *testing.T) {
	r, _ := newRegistry(t, false)
	r.Register("user", schemaJSON(`{"name":"id","type":"integer","required":true}`))
	v, err := r.LookupVersion("user", "latest")
	if err != nil || v != 1 {
		t.Fatalf("latest = %d, err %v", v, err)
	}
	v, err = r.LookupVersion("user", "1")
	if err != nil || v != 1 {
		t.Fatalf("1 = %d, err %v", v, err)
	}
	if _, err := r.LookupVersion("user", "x"); err == nil {
		t.Fatalf("expected error for non-numeric version")
	}
	if _, err := r.LookupVersion("nope", "latest"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
