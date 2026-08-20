package diff

import (
	"testing"

	"task102-schemaregistry/internal/schema"
)

func parse(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, err := schema.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return s
}

func TestComputeAddedRemoved(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"email","type":"string"}]}`)
	r := Compute(old, newer)
	if len(r.Added) != 1 || r.Added[0] != "email" {
		t.Fatalf("added = %v", r.Added)
	}
	if len(r.Removed) != 1 || r.Removed[0] != "name" {
		t.Fatalf("removed = %v", r.Removed)
	}
	if len(r.Changed) != 0 {
		t.Fatalf("expected no changes, got %+v", r.Changed)
	}
}

func TestComputeTypeChange(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"string","required":true}]}`)
	r := Compute(old, newer)
	if len(r.Changed) != 1 || r.Changed[0].Field != "id" || r.Changed[0].Kind != KindTypeChanged {
		t.Fatalf("changed = %+v", r.Changed)
	}
}

func TestComputeRequiredChange(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":false}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	r := Compute(old, newer)
	if len(r.Changed) != 1 || r.Changed[0].Kind != KindRequiredChanged {
		t.Fatalf("changed = %+v", r.Changed)
	}
}

func TestComputeEnumChange(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b"]}]}`)
	newer := parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b","c"]}]}`)
	r := Compute(old, newer)
	if len(r.Changed) != 1 || r.Changed[0].Kind != KindEnumChanged {
		t.Fatalf("changed = %+v", r.Changed)
	}
	// enum order-independent: no change if same set
	old2 := parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b"]}]}`)
	new2 := parse(t, `{"fields":[{"name":"r","type":"string","enum":["b","a"]}]}`)
	if len(Compute(old2, new2).Changed) != 0 {
		t.Fatalf("same enum set (different order) should not be a change")
	}
}

func TestComputeDefaultChange(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","default":0}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","default":5}]}`)
	r := Compute(old, newer)
	if len(r.Changed) != 1 || r.Changed[0].Kind != KindDefaultChanged {
		t.Fatalf("changed = %+v", r.Changed)
	}
	// default false vs no-default is a change
	old2 := parse(t, `{"fields":[{"name":"flag","type":"boolean"}]}`)
	new2 := parse(t, `{"fields":[{"name":"flag","type":"boolean","default":false}]}`)
	if len(Compute(old2, new2).Changed) != 1 {
		t.Fatalf("adding default false should be a change")
	}
}

func TestComputeNoChanges(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	r := Compute(old, newer)
	if r.HasChanges() {
		t.Fatalf("identical schemas should have no changes, got %+v", r)
	}
}

func TestComputeMultipleChangesSorted(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"z","type":"integer"},{"name":"a","type":"string","required":true},{"name":"m","type":"integer"}]}`)
	newer := parse(t, `{"fields":[{"name":"z","type":"number"},{"name":"a","type":"string","required":false},{"name":"m","type":"integer","default":0}]}`)
	r := Compute(old, newer)
	if len(r.Changed) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(r.Changed), r.Changed)
	}
	// changed entries sorted by field name
	if r.Changed[0].Field != "a" || r.Changed[1].Field != "m" || r.Changed[2].Field != "z" {
		t.Fatalf("changes not sorted by field: %+v", r.Changed)
	}
}
