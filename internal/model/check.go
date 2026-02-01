package model

// CheckStatus represents the status of a preflight check.
type CheckStatus string

const (
	// CheckStatusOK indicates the check passed.
	CheckStatusOK CheckStatus = "ok"
	// CheckStatusWarning indicates the check passed with a warning.
	CheckStatusWarning CheckStatus = "warning"
	// CheckStatusError indicates the check failed.
	CheckStatusError CheckStatus = "error"
)

// CheckResult represents the result of a single preflight check.
type CheckResult struct {
	ID      string      // Unique identifier for the check (e.g., "kvm_available").
	Message string      // Human-readable description of the result.
	Status  CheckStatus // Status of the check.
}

// HasErrors returns true if any check result has an error status.
func HasErrors(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == CheckStatusError {
			return true
		}
	}
	return false
}

// HasWarnings returns true if any check result has a warning status.
func HasWarnings(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == CheckStatusWarning {
			return true
		}
	}
	return false
}

// CountByStatus counts check results by status.
func CountByStatus(results []CheckResult) (ok, warnings, errors int) {
	for _, r := range results {
		switch r.Status {
		case CheckStatusOK:
			ok++
		case CheckStatusWarning:
			warnings++
		case CheckStatusError:
			errors++
		}
	}
	return
}
