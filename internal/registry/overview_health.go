package registry

func overviewHealthy(subjects []SubjectOverview) bool {
	for _, subject := range subjects {
		if subject.Name == "" || subject.Versions < 1 {
			return false
		}
	}
	return true
}
