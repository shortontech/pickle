package squeeze

import "fmt"

// Severity indicates the importance of a finding.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// Finding represents a single issue detected by squeeze.
type Finding struct {
	Rule     string
	Severity Severity
	File     string
	Line     int
	Message  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s: %s", f.File, f.Line, f.Severity, f.Rule, f.Message)
}
