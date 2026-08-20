package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func newMuxForAPI(t *testing.T) http.Handler {
	mux, _ := newTestMux(t)
	return mux
}

func TestReadyAndIndex(t *testing.T) {
	mux := newMuxForAPI(t)
	w := do(t, mux, "GET", "/ready", nil)
	if w.Code != 200 {
		t.Fatalf("ready status = %d", w.Code)
	}
	w = do(t, mux, "GET", "/", nil)
	if w.Code != 200 {
		t.Fatalf("index status = %d", w.Code)
	}
	var idx map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &idx)
	if idx["service"] != "schema-registry" {
		t.Fatalf("index service = %v", idx["service"])
	}
}

func TestSchemasFingerprint(t *testing.T) {
	mux := newMuxForAPI(t)
	w := do(t, mux, "POST", "/schemas/fingerprint",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]string
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["fingerprint"] == "" {
		t.Fatalf("fingerprint empty")
	}
}

func TestSchemasNormalize(t *testing.T) {
	mux := newMuxForAPI(t)
	w := do(t, mux, "POST", "/schemas/normalize",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["field_count"] == nil {
		t.Fatalf("normalize missing field_count: %s", w.Body.String())
	}
}

func TestSchemasValidateInline(t *testing.T) {
	mux := newMuxForAPI(t)
	body := map[string]interface{}{
		"schema":  map[string]interface{}{"fields": []map[string]interface{}{{"name": "id", "type": "integer", "required": true}}},
		"message": map[string]interface{}{"id": 1},
	}
	w := do(t, mux, "POST", "/schemas/validate", body)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["valid"] != true {
		t.Fatalf("expected valid, got %v", res["valid"])
	}
}

func TestSchemasCompatInline(t *testing.T) {
	mux := newMuxForAPI(t)
	body := map[string]interface{}{
		"old":  map[string]interface{}{"fields": []map[string]interface{}{{"name": "id", "type": "integer", "required": true}}},
		"new":  map[string]interface{}{"fields": []map[string]interface{}{{"name": "id", "type": "integer", "required": true}, {"name": "age", "type": "integer", "required": true}}},
		"mode": "BACKWARD",
	}
	w := do(t, mux, "POST", "/schemas/compatibility", body)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["compatible"] == true {
		t.Fatalf("expected incompatible")
	}
}

func TestSchemasDiff(t *testing.T) {
	mux := newMuxForAPI(t)
	body := map[string]interface{}{
		"old": map[string]interface{}{"fields": []map[string]interface{}{{"name": "id", "type": "integer", "required": true}, {"name": "name", "type": "string", "required": true}}},
		"new": map[string]interface{}{"fields": []map[string]interface{}{{"name": "id", "type": "string", "required": true}, {"name": "email", "type": "string"}}},
	}
	w := do(t, mux, "POST", "/schemas/diff", body)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	added, _ := res["added"].([]interface{})
	if len(added) != 1 || added[0] != "email" {
		t.Fatalf("added = %v", added)
	}
}

func TestGlobalConfig(t *testing.T) {
	mux := newMuxForAPI(t)
	w := do(t, mux, "GET", "/config", nil)
	if w.Code != 200 {
		t.Fatalf("get global config status = %d", w.Code)
	}
	w = do(t, mux, "PUT", "/config", map[string]interface{}{"compatibility": "FULL"})
	if w.Code != 200 {
		t.Fatalf("set global config status = %d body=%s", w.Code, w.Body.String())
	}
	w = do(t, mux, "GET", "/config", nil)
	var res map[string]string
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["default_compatibility"] != "FULL" {
		t.Fatalf("global config = %q", res["default_compatibility"])
	}
}

func TestSubjectInfoAndStats(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "GET", "/subjects/user", nil)
	if w.Code != 200 {
		t.Fatalf("subject info status = %d body=%s", w.Code, w.Body.String())
	}
	w = do(t, mux, "GET", "/subjects/user/stats", nil)
	if w.Code != 200 {
		t.Fatalf("stats status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetLatestByPath(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "GET", "/subjects/user/versions/latest", nil)
	if w.Code != 200 {
		t.Fatalf("get latest status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestVersionFingerprint(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "GET", "/subjects/user/versions/1/fingerprint", nil)
	if w.Code != 200 {
		t.Fatalf("fingerprint status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestCanonicalize(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
			{"name": "role", "type": "string", "enum": []string{"user", "admin"}, "default": "user"},
		}})
	w := do(t, mux, "POST", "/subjects/user/canonicalize?version=latest",
		map[string]interface{}{"id": 1, "extra": 99})
	if w.Code != 200 {
		t.Fatalf("canonicalize status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["valid"] != true {
		t.Fatalf("expected valid, got %v", res["valid"])
	}
}

func TestAudit(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "GET", "/subjects/user/audit", nil)
	if w.Code != 200 {
		t.Fatalf("audit status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestExport(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "GET", "/subjects/user/export", nil)
	if w.Code != 200 {
		t.Fatalf("export status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["subject"] != "user" {
		t.Fatalf("export subject = %v", res["subject"])
	}
}

func TestCheckCompatVersion(t *testing.T) {
	mux := newMuxForAPI(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "POST", "/compatibility/subjects/user/versions/1",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
			{"name": "age", "type": "integer", "required": true},
		}})
	if w.Code != 200 {
		t.Fatalf("check compat version status = %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["compatible"] == true {
		t.Fatalf("expected incompatible")
	}
}
