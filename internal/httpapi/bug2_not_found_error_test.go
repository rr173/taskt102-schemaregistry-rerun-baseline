package httpapi

import "testing"

func TestMissingCompatibilityVersionIsNotFound(t *testing.T) {
	mux, _ := newTestMux(t)
	w := do(t, mux, "POST", "/compatibility/subjects/missing/versions/9", map[string]interface{}{"fields": []interface{}{}})
	if w.Code != 404 { t.Fatalf("missing version should be 404, got %d body=%s", w.Code, w.Body.String()) }
}
