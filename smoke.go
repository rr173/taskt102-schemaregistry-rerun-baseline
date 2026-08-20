package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"task102-schemaregistry/internal/httpapi"
	"task102-schemaregistry/internal/registry"
	"task102-schemaregistry/internal/store"
)

// runSmoke exercises the registry end-to-end: register, evolve, idempotency,
// compatibility, validation, config, delete, restart recovery and a live HTTP
// round-trip. It returns nil on success.
func runSmoke() error {
	dir, err := os.MkdirTemp("", "schemareg-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "registry.db")

	// Phase 1: register + evolve + idempotent + validate + compat + config + delete.
	s, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	r := registry.New(s)
	reg := func(subject string, def string) (int, error) {
		res, err := r.Register(subject, []byte(def))
		if err != nil {
			return 0, err
		}
		return res.Version, nil
	}
	v1def := `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true},{"name":"role","type":"string","enum":["user","admin"],"default":"user"}]}`
	v2def := `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true},{"name":"role","type":"string","enum":["user","admin"],"default":"user"},{"name":"email","type":"string","required":false,"default":""}]}`

	if v, err := reg("user", v1def); err != nil || v != 1 {
		return fmt.Errorf("register v1: v=%d err=%v", v, err)
	}
	if v, err := reg("user", v2def); err != nil || v != 2 {
		return fmt.Errorf("register v2: v=%d err=%v", v, err)
	}
	// incompatible: add required no default
	if _, err := r.Register("user", []byte(`{"fields":[{"name":"id","type":"integer","required":true},{"name":"age","type":"integer","required":true}]}`)); err == nil {
		return fmt.Errorf("expected compat failure for age")
	}
	// idempotent: resubmit v2 with reordered fields
	idem := `{"fields":[{"name":"email","type":"string","required":false,"default":""},{"name":"role","type":"string","enum":["user","admin"],"default":"user"},{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}]}`
	res, err := r.Register("user", []byte(idem))
	if err != nil || !res.Idempotent || res.Version != 2 {
		return fmt.Errorf("idempotent: %+v err=%v", res, err)
	}
	// validate
	vr, err := r.Validate("user", 2, []byte(`{"id":1,"name":"a","email":"x"}`))
	if err != nil || !vr.Valid {
		return fmt.Errorf("validate valid: %+v err=%v", vr, err)
	}
	vr, err = r.Validate("user", 2, []byte(`{"name":"a"}`))
	if err == nil && vr.Valid {
		return fmt.Errorf("validate missing id should be invalid")
	}
	vr, err = r.Validate("user", 2, []byte(`{"id":1,"name":"a","role":"superuser"}`))
	if err == nil && vr.Valid {
		return fmt.Errorf("validate enum oob should be invalid")
	}
	// compat试探
	cr, err := r.CheckCompat("user", []byte(`{"fields":[{"name":"id","type":"integer","required":true},{"name":"age","type":"integer","required":true}]}`))
	if err != nil || cr.Compatible {
		return fmt.Errorf("checkcompat should be incompatible: %+v", cr)
	}
	// config NONE then register incompatible
	if err := r.SetConfig("user", "NONE"); err != nil {
		return fmt.Errorf("setconfig: %v", err)
	}
	if v, err := reg("user", `{"fields":[{"name":"id","type":"string","required":true}]}`); err != nil || v != 3 {
		return fmt.Errorf("register v3 under NONE: v=%d err=%v", v, err)
	}
	// delete non-latest rejected
	if err := r.DeleteVersion("user", 1); err == nil {
		return fmt.Errorf("delete non-latest should fail")
	}
	// delete latest
	if err := r.DeleteVersion("user", 3); err != nil {
		return fmt.Errorf("delete latest: %v", err)
	}
	// version not reused
	if v, err := reg("user", `{"fields":[{"name":"id","type":"number","required":true}]}`); err != nil || v != 4 {
		return fmt.Errorf("register v4 (no reuse): v=%d err=%v", v, err)
	}
	if err := s.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	// Phase 2: restart recovery.
	s2, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("reopen store: %w", err)
	}
	r2 := registry.New(s2)
	subs, err := r2.ListSubjects()
	if err != nil || len(subs) != 1 || subs[0].Name != "user" || subs[0].Compatibility != "NONE" {
		return fmt.Errorf("recovery subjects: %+v err=%v", subs, err)
	}
	versions, err := r2.ListVersions("user")
	if err != nil || len(versions) != 3 { // 1,2,4 (3 was deleted)
		return fmt.Errorf("recovery versions: %v err=%v", versions, err)
	}
	if versions[0] != 1 || versions[1] != 2 || versions[2] != 4 {
		return fmt.Errorf("recovery version numbers: %v", versions)
	}
	// idempotent still works after restart
	res2, err := r2.Register("user", []byte(`{"fields":[{"name":"id","type":"number","required":true}]}`))
	if err != nil || !res2.Idempotent || res2.Version != 4 {
		return fmt.Errorf("recovery idempotent: %+v err=%v", res2, err)
	}
	s2.Close()

	// Phase 3: live HTTP round-trip via httptest.
	s3, err := store.Open(filepath.Join(dir, "http.db"))
	if err != nil {
		return err
	}
	defer s3.Close()
	mux := httpapi.NewMux(registry.New(s3))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	body, _ := json.Marshal(map[string]interface{}{"fields": []map[string]interface{}{
		{"name": "id", "type": "integer", "required": true},
	}})
	resp, err := http.Post(srv.URL+"/subjects/probe/versions", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http register status = %d", resp.StatusCode)
	}
	return nil
}
