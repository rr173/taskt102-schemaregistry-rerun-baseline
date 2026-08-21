package schema

import "testing"

func TestLargeIntegerPrecision(t *testing.T) {
	s, err := Parse([]byte(`{"fields":[{"name":"id","type":"integer","default":9007199254740993}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(s.Fields[0].Default); got != "9007199254740993" {
		t.Fatalf("large integer default lost precision: %s", got)
	}
}

// TestLargeIntegerPrecisionEdgeCases covers values that an int64/float64
// path would silently mangle: the JS safe boundary, a negative value past it,
// and a value far beyond int64 range. The registry must preserve the exact
// decimal text supplied by the contract author.
func TestLargeIntegerPrecisionEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"max-safe", "9007199254740991", "9007199254740991"},        // 2^53 - 1
		{"just-past-safe", "9007199254740993", "9007199254740993"}, // 2^53 + 1
		{"negative-past-safe", "-9007199254740993", "-9007199254740993"},
		{"beyond-int64", "9223372036854775808", "9223372036854775808"}, // 2^63
		{"far-beyond", "123456789012345678901234567890", "123456789012345678901234567890"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Parse([]byte(`{"fields":[{"name":"id","type":"integer","default":` + c.raw + `}]}`))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if got := string(s.Fields[0].Default); got != c.want {
				t.Fatalf("default = %s, want %s", got, c.want)
			}
			// DefinitionJSON round-trips through Parse again, so the stored
			// default must reparse to the identical value.
			b, err := s.DefinitionJSON()
			if err != nil {
				t.Fatal(err)
			}
			s2, err := Parse(b)
			if err != nil {
				t.Fatalf("round-trip parse error: %v", err)
			}
			if got := string(s2.Fields[0].Default); got != c.want {
				t.Fatalf("round-trip default = %s, want %s", got, c.want)
			}
		})
	}
}
