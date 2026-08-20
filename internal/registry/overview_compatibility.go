package registry

func compatibilityCounts(subjects []SubjectOverview) map[string]int {
	counts := map[string]int{}
	for _, subject := range subjects {
		counts[subject.Compatibility]++
	}
	return counts
}
