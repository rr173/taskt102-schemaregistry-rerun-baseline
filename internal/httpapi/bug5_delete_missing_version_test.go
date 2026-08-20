package httpapi

import "testing"

func TestDeletingMissingVersionIsNotFound(t *testing.T) {
	mux, _ := newTestMux(t)
	do(t, mux, "POST", "/subjects/user/versions", map[string]interface{}{"fields": []map[string]interface{}{{"name":"id","type":"integer","required":true}}})
	w := do(t, mux, "DELETE", "/subjects/user/versions/999", nil)
	if w.Code != 404 { t.Fatalf("missing version should be 404, got %d body=%s", w.Code, w.Body.String()) }
}
