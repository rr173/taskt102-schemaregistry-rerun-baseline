package registry

// Overview returns the persisted registry health summary used by operators.
// It intentionally composes the same subject, version, audit and schema
// readers as the public endpoints, so the report also exercises recovery data.
func (r *Registry) Overview() (*Overview, error) {
	infos, err := r.ListSubjects()
	if err != nil {
		return nil, err
	}
	out := &Overview{Subjects: make([]SubjectOverview, 0, len(infos))}
	for _, info := range infos {
		subject, err := r.overviewSubject(info)
		if err != nil {
			return nil, err
		}
		subject.RequiredFields = r.requiredFields(subject.Name, subject.Versions)
		subject.LatestFingerprint = r.latestFingerprint(subject.Name, subject.Versions)
		subject.DuplicateVersions = r.duplicateVersions(subject.Name)
		subject.AuditEvents, err = r.auditCount(subject.Name)
		if err != nil {
			return nil, err
		}
		out.Subjects = append(out.Subjects, subject)
	}
	out.TotalVersions = versionCount(out.Subjects)
	for _, subject := range out.Subjects {
		out.TotalAuditEvents += subject.AuditEvents
	}
	out.Compatibility = compatibilityCounts(out.Subjects)
	out.Healthy = overviewHealthy(out.Subjects)
	return out, nil
}
