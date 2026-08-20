package registry

func versionCount(subjects []SubjectOverview) int {
	total := 0
	for _, subject := range subjects {
		total += subject.Versions
	}
	return total
}
