package compat

import (
	"testing"
	"task102-schemaregistry/internal/schema"
)

func TestCompatibilityViolationUsesSortedFieldOrder(t *testing.T) {
	old, err := schema.Parse([]byte(`{"fields":[{"name":"a","type":"integer"},{"name":"b","type":"integer"}]}`)); if err != nil { t.Fatal(err) }
	newer, err := schema.Parse([]byte(`{"fields":[{"name":"a","type":"string"},{"name":"b","type":"string"}]}`)); if err != nil { t.Fatal(err) }
	ok, violation := Check(Backward, old, newer)
	if ok || violation.Field != "a" { t.Fatalf("expected first violation on a: ok=%v v=%+v", ok, violation) }
}
