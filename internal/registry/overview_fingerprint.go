package registry

func (r *Registry) latestFingerprint(subject string, version int) string {
	row, err := r.store.GetSchema(subject, version)
	if err != nil {
		return ""
	}
	return row.Fingerprint
}
