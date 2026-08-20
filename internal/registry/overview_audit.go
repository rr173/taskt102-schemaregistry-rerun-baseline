package registry

func (r *Registry) auditCount(subject string) (int, error) {
	rows, err := r.Audit(subject, 1000)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}
