package registry

func (r *Registry) duplicateVersions(subject string) int {
	export, err := r.ExportSubject(subject)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	duplicates := 0
	for _, version := range export.Versions {
		if seen[version.Fingerprint] {
			duplicates++
		}
		seen[version.Fingerprint] = true
	}
	return duplicates
}
