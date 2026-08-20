package schema

import "testing"

func TestLargeIntegerPrecision(t *testing.T) {
	s, err := Parse([]byte(`{"fields":[{"name":"id","type":"integer","default":9007199254740993}]}`))
	if err != nil { t.Fatal(err) }
	if got := string(s.Fields[0].Default); got != "9007199254740993" {
		t.Fatalf("large integer default lost precision: %s", got)
	}
}
