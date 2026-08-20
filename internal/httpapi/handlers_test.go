package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"task102-schemaregistry/internal/registry"
	"task102-schemaregistry/internal/store"
)

func newTestMux(t *testing.T) (http.Handler, *registry.Registry) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	r := registry.New(s)
	return NewMux(r), r
}

func do(t *testing.T, mux http.Handler, method, target string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestHealth(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(t, mux, "GET", "/health", nil)
	if w.Code != 200 {
		t.Fatalf("health status = %d", w.Code)
	}
}

func TestRegisterAndGet(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	if w.Code != 200 {
		t.Fatalf("register status = %d body=%s", w.Code, w.Body.String())
	}
	var res registry.RegisterResult
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Version != 1 {
		t.Fatalf("version = %d", res.Version)
	}
	w = do(t, mux, "GET", "/subjects/user/versions/1", nil)
	if w.Code != 200 {
		t.Fatalf("get status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestRegisterCompatFail(t *testing.T) {
	mux, _ := newTestMux(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	// incompatible: add required no default
	w := do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
			{"name": "age", "type": "integer", "required": true},
		}})
	if w.Code != 409 {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	var eb errBody
	json.Unmarshal(w.Body.Bytes(), &eb)
	if eb.Error.Field != "age" {
		t.Fatalf("expected field age, got %+v", eb.Error)
	}
}

func TestValidate(t *testing.T) {
	mux, _ := newTestMux(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
			{"name": "role", "type": "string", "enum": []string{"user", "admin"}},
		}})
	w := do(t, mux, "POST", "/subjects/user/validate?version=latest",
		map[string]interface{}{"id": 1, "role": "superuser"})
	if w.Code != 200 {
		t.Fatalf("validate status = %d body=%s", w.Code, w.Body.String())
	}
	var res registry.ValidationResult
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Valid {
		t.Fatalf("expected invalid, got valid")
	}
}

func TestCheckCompat(t *testing.T) {
	mux, _ := newTestMux(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	w := do(t, mux, "POST", "/compatibility/subjects/user/versions/latest",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
			{"name": "age", "type": "integer", "required": true},
		}})
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res registry.CompatResult
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Compatible {
		t.Fatalf("expected incompatible")
	}
}

func TestConfig(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(t, mux, "PUT", "/config/user",
		map[string]interface{}{"compatibility": "NONE"})
	if w.Code != 200 {
		t.Fatalf("set config status = %d body=%s", w.Code, w.Body.String())
	}
	w = do(t, mux, "GET", "/config/user", nil)
	if w.Code != 200 {
		t.Fatalf("get config status = %d", w.Code)
	}
}

func TestNotFound(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(t, mux, "GET", "/subjects/nope/versions/1", nil)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	w = do(t, mux, "GET", "/subjects/nope/versions", nil)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteVersionNotLatest(t *testing.T) {
	mux, _ := newTestMux(t)
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "integer", "required": true},
		}})
	do(t, mux, "PUT", "/config/user", map[string]interface{}{"compatibility": "NONE"})
	do(t, mux, "POST", "/subjects/user/versions",
		map[string]interface{}{"fields": []map[string]interface{}{
			{"name": "id", "type": "string", "required": true},
		}})
	w := do(t, mux, "DELETE", "/subjects/user/versions/1", nil)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
