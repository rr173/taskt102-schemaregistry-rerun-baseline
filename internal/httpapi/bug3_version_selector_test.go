package httpapi

import "testing"

func TestMalformedVersionSelectorIsRejected(t *testing.T) {
	mux, _ := newTestMux(t)
	do(t, mux, "POST", "/subjects/user/versions", map[string]interface{}{"fields": []map[string]interface{}{{"name":"id","type":"integer","required":true}}})
	w := do(t, mux, "GET", "/subjects/user/versions/1junk", nil)
	if w.Code != 400 { t.Fatalf("malformed version selector should be 400, got %d body=%s", w.Code, w.Body.String()) }
}
