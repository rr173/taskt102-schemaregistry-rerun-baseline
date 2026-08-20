package registry

import "testing"

func TestOverviewReportsRequiredFieldCount(t *testing.T) {
	r, _ := newRegistry(t, false)
	if _, err := r.Register("users", schemaJSON(`{"name":"id","type":"integer","required":true},{"name":"name","type":"string"}`)); err != nil { t.Fatal(err) }
	view, err := r.Overview(); if err != nil { t.Fatal(err) }
	if len(view.Subjects) != 1 || view.Subjects[0].RequiredFields != 1 { t.Fatalf("expected one required field: %+v", view) }
}
