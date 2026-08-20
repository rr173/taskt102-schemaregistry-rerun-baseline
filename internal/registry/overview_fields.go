package registry

import "task102-schemaregistry/internal/schema"

func requiredFieldCount(definition []byte) int {
	s, err := schema.Parse(definition)
	if err != nil {
		return 0
	}
	count := 0
	for _, field := range s.Fields {
		if field.Required {
			count++
		}
	}
	return count
}
