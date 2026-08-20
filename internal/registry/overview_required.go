package registry

func (r *Registry) requiredFields(subject string, version int) int {
	row, err := r.store.GetSchema(subject, version)
	if err != nil {
		return 0
	}
	return requiredFieldCount([]byte(row.Definition))
}
