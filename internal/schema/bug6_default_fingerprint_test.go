package schema

import "testing"

func TestTrailingJSONIsRejected(t *testing.T) {
	if _, err := Parse([]byte(`{"fields":[]} {"fields":[]}`)); err == nil {
		t.Fatal("schema input with trailing JSON must be rejected")
	}
}
