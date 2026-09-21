// Package contract mirrors sicp-api-design.md sections 4–7. Validation belongs
// to P06; these transport types do not implement any pending business rule.
package contract

import "encoding/json"

// Array ensures non-null response arrays also serialize correctly at zero value.
type Array[T any] []T

func (a Array[T]) MarshalJSON() ([]byte, error) {
	if a == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]T(a))
}

type GradeFilters struct {
	Semester             *string  `json:"semester"`
	CourseCode           *string  `json:"course_code"`
	CourseName           *string  `json:"course_name"`
	CourseAttributeCodes []string `json:"course_attribute_codes"`
	CourseNatureCodes    []string `json:"course_nature_codes"`
	CourseTypeCodes      []string `json:"course_type_codes"`
	CourseDepartmentCode *string  `json:"course_department_code"`
	CourseDepartmentName *string  `json:"course_department_name"`
	ScoreMin             *float64 `json:"score_min"`
	ScoreMax             *float64 `json:"score_max"`
	Page                 *int     `json:"page"`
	PageSize             *int     `json:"page_size"`
}
type StudentGradesRequest struct {
	StudentNumber string  `json:"student_number"`
	AcademicYear  *string `json:"academic_year"`
	GradeFilters
}
type BatchGradesRequest struct {
	StudentNumbers        []string `json:"student_numbers"`
	StudentDepartmentCode *string  `json:"student_department_code"`
	StudentDepartmentName *string  `json:"student_department_name"`
	ClassCode             *string  `json:"class_code"`
	ClassName             *string  `json:"class_name"`
	AcademicYear          string   `json:"academic_year"`
	GradeFilters
}
type StudentTimetableRequest struct {
	StudentNumber string  `json:"student_number"`
	AcademicYear  string  `json:"academic_year"`
	Semester      string  `json:"semester"`
	CourseQuery   *string `json:"course_query"`
}
type StaffTimetableRequest struct {
	StaffNumber         string  `json:"staff_number"`
	AcademicYear        string  `json:"academic_year"`
	Semester            string  `json:"semester"`
	CourseQuery         *string `json:"course_query"`
	TeachingClassNumber *string `json:"teaching_class_number"`
}
type GradesResponse struct {
	Total    int          `json:"total"`
	SyncedAt string       `json:"synced_at"`
	Items    Array[Grade] `json:"items"`
}
type Grade struct {
	GradeID         string       `json:"grade_id"`
	Student         GradeStudent `json:"student"`
	AcademicYear    string       `json:"academic_year"`
	Semester        string       `json:"semester"`
	Course          Course       `json:"course"`
	Score           *float64     `json:"score"`
	GradePoint      *float64     `json:"grade_point"`
	Credits         *float64     `json:"credits"`
	SourceUpdatedAt *string      `json:"source_updated_at"`
}
type GradeStudent struct {
	StudentNumber  string  `json:"student_number"`
	Name           *string `json:"name"`
	DepartmentCode *string `json:"department_code"`
	DepartmentName *string `json:"department_name"`
	ClassCode      *string `json:"class_code"`
	ClassName      *string `json:"class_name"`
}
type Course struct {
	CourseCode       string            `json:"course_code"`
	CourseName       *string           `json:"course_name"`
	AttributeCode    *string           `json:"attribute_code"`
	AttributeName    *string           `json:"attribute_name"`
	NatureCode       *string           `json:"nature_code"`
	NatureName       *string           `json:"nature_name"`
	TypeCode         *string           `json:"type_code"`
	TypeName         *string           `json:"type_name"`
	Department       *CourseDepartment `json:"department"`
	TotalHours       *float64          `json:"total_hours"`
	RetakeFlag       *string           `json:"retake_flag"`
	RetakeCourseCode *string           `json:"retake_course_code"`
	RetakeCourseName *string           `json:"retake_course_name"`
}
type CourseDepartment struct {
	OrganizationCode string  `json:"organization_code"`
	OrganizationName *string `json:"organization_name"`
}
type TimetableEntry struct {
	CourseCode          string        `json:"course_code"`
	CourseName          *string       `json:"course_name"`
	PlannedCourseCode   string        `json:"planned_course_code"`
	TeachingClassNumber string        `json:"teaching_class_number"`
	TeachingClassName   *string       `json:"teaching_class_name"`
	EnrolledCount       *int          `json:"enrolled_count"`
	TeacherStaffNumbers Array[string] `json:"teacher_staff_numbers"`
	RoomCode            *string       `json:"room_code"`
	Credits             *float64      `json:"credits"`
	TeacherNames        Array[string] `json:"teacher_names"`
	Location            *string       `json:"location"`
	Weekday             int           `json:"weekday"`
	Sections            Array[int]    `json:"sections"`
	Weeks               Array[int]    `json:"weeks"`
	Dates               Array[string] `json:"dates"`
	SourceUpdatedAt     *string       `json:"source_updated_at"`
}
type StudentTimetableResponse struct {
	StudentNumber string                `json:"student_number"`
	AcademicYear  string                `json:"academic_year"`
	Semester      string                `json:"semester"`
	SyncedAt      string                `json:"synced_at"`
	Items         Array[TimetableEntry] `json:"items"`
}
type StaffTimetableResponse struct {
	StaffNumber  string                `json:"staff_number"`
	AcademicYear string                `json:"academic_year"`
	Semester     string                `json:"semester"`
	SyncedAt     string                `json:"synced_at"`
	Items        Array[TimetableEntry] `json:"items"`
}
