package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T, persist bool) *Store {
	t.Helper()
	var path string
	if persist {
		path = filepath.Join(t.TempDir(), "registry.db")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenEnsureSchema(t *testing.T) {
	s := newTestStore(t, false)
	if err := s.ensureSchema(); err != nil {
		t.Fatalf("re-run ensureSchema should be idempotent: %v", err)
	}
}

func TestRegisterVersionAndAdvance(t *testing.T) {
	s := newTestStore(t, false)
	v, err := s.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp1", "t1")
	if err != nil {
		t.Fatalf("RegisterVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("first version should be 1, got %d", v)
	}
	v2, err := s.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp2", "t2")
	if err != nil {
		t.Fatalf("RegisterVersion 2: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("second version should be 2, got %d", v2)
	}
	subj, err := s.GetSubject("user")
	if err != nil {
		t.Fatalf("GetSubject: %v", err)
	}
	if subj.Compatibility != "BACKWARD" {
		t.Fatalf("compatibility should be BACKWARD, got %s", subj.Compatibility)
	}
	if subj.NextVersion != 3 {
		t.Fatalf("next_version should be 3, got %d", subj.NextVersion)
	}
	latest, err := s.LatestVersion("user")
	if err != nil {
		t.Fatal(err)
	}
	if latest != 2 {
		t.Fatalf("latest should be 2, got %d", latest)
	}
	versions, err := s.ListVersions("user")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("versions = %v", versions)
	}
}

func TestRegisterVersionUsesGivenCompat(t *testing.T) {
	s := newTestStore(t, false)
	if _, err := s.RegisterVersion("cfg", "FULL", `{"fields":[]}`, "fp1", "t1"); err != nil {
		t.Fatal(err)
	}
	subj, _ := s.GetSubject("cfg")
	if subj.Compatibility != "FULL" {
		t.Fatalf("new subject should inherit FULL, got %s", subj.Compatibility)
	}
	// re-register does not overwrite existing subject's compatibility
	if _, err := s.RegisterVersion("cfg", "NONE", `{"fields":[]}`, "fp2", "t2"); err != nil {
		t.Fatal(err)
	}
	subj, _ = s.GetSubject("cfg")
	if subj.Compatibility != "FULL" {
		t.Fatalf("existing subject compatibility must not change, got %s", subj.Compatibility)
	}
}

func TestNotFoundErrors(t *testing.T) {
	s := newTestStore(t, false)
	if _, err := s.GetSubject("nope"); err != ErrNotFound {
		t.Fatalf("GetSubject missing should be ErrNotFound, got %v", err)
	}
	if _, err := s.GetSchema("nope", 1); err != ErrNotFound {
		t.Fatalf("GetSchema missing should be ErrNotFound, got %v", err)
	}
	if _, err := s.LatestSchema("nope"); err != ErrNotFound {
		t.Fatalf("LatestSchema missing should be ErrNotFound, got %v", err)
	}
	if err := s.DeleteSubject("nope"); err != ErrNotFound {
		t.Fatalf("DeleteSubject missing should be ErrNotFound, got %v", err)
	}
	if err := s.DeleteVersion("nope", 1); err != ErrNotFound {
		t.Fatalf("DeleteVersion missing should be ErrNotFound, got %v", err)
	}
}

func TestDeleteVersionAndSubject(t *testing.T) {
	s := newTestStore(t, false)
	if _, err := s.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp1", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp2", "t2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteVersion("user", 2); err != nil {
		t.Fatalf("DeleteVersion(2): %v", err)
	}
	latest, _ := s.LatestVersion("user")
	if latest != 1 {
		t.Fatalf("after deleting v2, latest should be 1, got %d", latest)
	}
	// next_version is not reused (high-water mark)
	v, err := s.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp3", "t3")
	if err != nil {
		t.Fatal(err)
	}
	if v != 3 {
		t.Fatalf("after delete, next registered version should be 3 (no reuse), got %d", v)
	}
	if err := s.DeleteSubject("user"); err != nil {
		t.Fatalf("DeleteSubject: %v", err)
	}
	if err := s.DeleteSubject("user"); err != ErrNotFound {
		t.Fatalf("second DeleteSubject should be ErrNotFound, got %v", err)
	}
	versions, _ := s.ListVersions("user")
	if len(versions) != 0 {
		t.Fatalf("versions should be empty after subject delete, got %v", versions)
	}
}

func TestSetCompatibilityEnsuresSubject(t *testing.T) {
	s := newTestStore(t, false)
	if err := s.SetCompatibility("cfg", "FORWARD", "t1"); err != nil {
		t.Fatalf("SetCompatibility: %v", err)
	}
	subj, err := s.GetSubject("cfg")
	if err != nil {
		t.Fatal(err)
	}
	if subj.Compatibility != "FORWARD" {
		t.Fatalf("compatibility should be FORWARD, got %s", subj.Compatibility)
	}
	if subj.NextVersion != 1 {
		t.Fatalf("next_version should be 1, got %d", subj.NextVersion)
	}
}

func TestMeta(t *testing.T) {
	s := newTestStore(t, false)
	// unset key returns empty string, no error
	v, err := s.GetMeta("default_compatibility")
	if err != nil || v != "" {
		t.Fatalf("unset meta should be ('', nil), got (%q, %v)", v, err)
	}
	if err := s.SetMeta("default_compatibility", "FULL"); err != nil {
		t.Fatal(err)
	}
	v, err = s.GetMeta("default_compatibility")
	if err != nil || v != "FULL" {
		t.Fatalf("meta should be FULL, got (%q, %v)", v, err)
	}
	// upsert
	if err := s.SetMeta("default_compatibility", "NONE"); err != nil {
		t.Fatal(err)
	}
	v, _ = s.GetMeta("default_compatibility")
	if v != "NONE" {
		t.Fatalf("meta upsert failed, got %q", v)
	}
}

func TestRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	if _, err := s1.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp1", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.RegisterVersion("user", "BACKWARD", `{"fields":[]}`, "fp2", "t2"); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetCompatibility("user", "FULL", "t0"); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetMeta("default_compatibility", "FORWARD"); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	t.Cleanup(func() { s2.Close() })

	subj, err := s2.GetSubject("user")
	if err != nil {
		t.Fatalf("GetSubject after restart: %v", err)
	}
	if subj.Compatibility != "FULL" {
		t.Fatalf("compatibility not recovered: %s", subj.Compatibility)
	}
	if subj.NextVersion != 3 {
		t.Fatalf("next_version not recovered: %d", subj.NextVersion)
	}
	versions, _ := s2.ListVersions("user")
	if len(versions) != 2 {
		t.Fatalf("versions not recovered: %v", versions)
	}
	got, err := s2.GetSchema("user", 2)
	if err != nil {
		t.Fatalf("GetSchema after restart: %v", err)
	}
	if got.Fingerprint != "fp2" {
		t.Fatalf("fingerprint not recovered: %s", got.Fingerprint)
	}
	meta, _ := s2.GetMeta("default_compatibility")
	if meta != "FORWARD" {
		t.Fatalf("meta not recovered: %q", meta)
	}
}
