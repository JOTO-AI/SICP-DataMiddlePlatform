package contract

type Operation string

const (
	Organizations         Operation = "organizations.query"
	Students              Operation = "students.query"
	Staff                 Operation = "staff.query"
	StaffTeachingClasses  Operation = "staff.teaching-classes.query"
	TeachingClassStudents Operation = "teaching-classes.students.query"

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
		{StudentGrades, "/api/v1/sicp/students/grades/query", nil},
		{BatchGrades, "/api/v1/sicp/students/grades/batch-query", nil},
		{StudentTimetable, "/api/v1/sicp/students/timetable/query", nil},
		{StaffTimetable, "/api/v1/sicp/staff/timetable/query", nil},
	}
}

// Categories are the fixed public codes defined by the query transport contract.
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
	ResourceExhausted ErrorCategory = "resource_exhausted"
)
