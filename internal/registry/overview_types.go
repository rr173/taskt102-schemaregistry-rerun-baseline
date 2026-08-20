package registry

type SubjectOverview struct {
	Name              string `json:"name"`
	Compatibility     string `json:"compatibility"`
	Versions          int    `json:"versions"`
	LatestFields      int    `json:"latest_fields"`
	RequiredFields    int    `json:"required_fields"`
	AuditEvents       int    `json:"audit_events"`
	LatestFingerprint string `json:"latest_fingerprint,omitempty"`
	DuplicateVersions int    `json:"duplicate_versions"`
}

type Overview struct {
	Subjects         []SubjectOverview `json:"subjects"`
	Compatibility    map[string]int    `json:"compatibility"`
	TotalVersions    int               `json:"total_versions"`
	TotalAuditEvents int               `json:"total_audit_events"`
	Healthy          bool              `json:"healthy"`
}
