package registry

func (r *Registry) overviewSubject(info SubjectInfo) (SubjectOverview, error) {
	stats, err := r.Stats(info.Name)
	if err != nil {
		return SubjectOverview{}, err
	}
	return SubjectOverview{Name: info.Name, Compatibility: info.Compatibility, Versions: stats.VersionCount, LatestFields: stats.LatestFieldCount, LatestFingerprint: stats.LatestFingerprint}, nil
}
