package contract

type Operation string

const (
	StudentGrades    Operation = "students.grades.query"
	BatchGrades      Operation = "students.grades.batch-query"
	StudentTimetable Operation = "students.timetable.query"
	StaffTimetable   Operation = "staff.timetable.query"
)

type Definition struct {
	Operation    Operation
	Path         string
	PendingRules []string
}

// Definitions returns fresh values to keep callers from mutating the catalog.
func Definitions() []Definition {
	return []Definition{
		{StudentGrades, "/api/v1/sicp/students/grades/query", []string{"non_numeric_score", "duplicate_course", "historical_course"}},
		{BatchGrades, "/api/v1/sicp/students/grades/batch-query", []string{"non_numeric_score", "duplicate_course", "historical_course"}},
		{StudentTimetable, "/api/v1/sicp/students/timetable/query", []string{"unscheduled_course", "duplicate_course", "timetable_formats"}},
		{StaffTimetable, "/api/v1/sicp/staff/timetable/query", []string{"duplicate_course", "timetable_formats"}},
	}
}

// Categories are internal classifications, not the final P06 response envelope.
type ErrorCategory string

const (
	InvalidArgument   ErrorCategory = "invalid_argument"
	Unauthenticated   ErrorCategory = "unauthenticated"
	Forbidden         ErrorCategory = "forbidden"
	NotReady          ErrorCategory = "not_ready"
	Conflict          ErrorCategory = "conflict"
	DependencyFailure ErrorCategory = "dependency_failure"
	DeadlineExceeded  ErrorCategory = "deadline_exceeded"
	Internal          ErrorCategory = "internal"
)
