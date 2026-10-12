package contract

// PaginationRequest preserves omitted/null values until operation-specific
// validation supplies defaults. All phase-two queries support pagination.
type PaginationRequest struct {
	Page     *int `json:"page"`
	PageSize *int `json:"page_size"`
}

type OrganizationsRequest struct {
	PaginationRequest
	OrganizationCode       *string  `json:"organization_code"`
	OrganizationName       *string  `json:"organization_name"`
	ParentOrganizationCode *string  `json:"parent_organization_code"`
	CategoryCodes          []string `json:"category_codes"`
	IsActive               *bool    `json:"is_active"`
}

type StudentsRequest struct {
	PaginationRequest
	StudentNumber          *string  `json:"student_number"`
	Name                   *string  `json:"name"`
	DepartmentCode         *string  `json:"department_code"`
	DepartmentName         *string  `json:"department_name"`
	CampusCode             *string  `json:"campus_code"`
	MajorCode              *string  `json:"major_code"`
	Grade                  *string  `json:"grade"`
	ClassCode              *string  `json:"class_code"`
	StatusCodes            []string `json:"status_codes"`
	EnrollmentStatusCodes  []string `json:"enrollment_status_codes"`
	OnCampusFlag           *string  `json:"on_campus_flag"`
	StudentCategoryCodes   []string `json:"student_category_codes"`
	StudyYears             *int     `json:"study_years"`
	AdmissionYear          *string  `json:"admission_year"`
	ExpectedGraduationYear *string  `json:"expected_graduation_year"`
	CounselorStaffNumber   *string  `json:"counselor_staff_number"`
}

type StaffRequest struct {
	PaginationRequest
	StaffNumber    *string  `json:"staff_number"`
	Name           *string  `json:"name"`
	DepartmentCode *string  `json:"department_code"`
	DepartmentName *string  `json:"department_name"`
	StatusCodes    []string `json:"status_codes"`
}

type StaffTeachingClassesRequest struct {
	PaginationRequest
	StaffNumber  string  `json:"staff_number"`
	AcademicYear string  `json:"academic_year"`
	Semester     string  `json:"semester"`
	CourseCode   *string `json:"course_code"`
}

type TeachingClassStudentsRequest struct {
	PaginationRequest
	TeachingClassNumber string `json:"teaching_class_number"`
	PlannedCourseCode   string `json:"planned_course_code"`
	AcademicYear        string `json:"academic_year"`
	Semester            string `json:"semester"`
}

type OrganizationsResponse struct {
	Total    int                 `json:"total"`
	SyncedAt string              `json:"synced_at"`
	Items    Array[Organization] `json:"items"`
}

type Organization struct {
	OrganizationCode       string  `json:"organization_code"`
	OrganizationName       string  `json:"organization_name"`
	ParentOrganizationCode *string `json:"parent_organization_code"`
	CategoryCode           *string `json:"category_code"`
	CategoryName           *string `json:"category_name"`
	IsActive               bool    `json:"is_active"`
	SourceUpdatedAt        *string `json:"source_updated_at"`
}

// Department distinguishes a missing organization (nil IsActive) from an
// organization whose source status is inactive or unknown (false).
type Department struct {
	OrganizationCode       string  `json:"organization_code"`
	OrganizationName       *string `json:"organization_name"`
	ParentOrganizationCode *string `json:"parent_organization_code"`
	CategoryCode           *string `json:"category_code"`
	CategoryName           *string `json:"category_name"`
	IsActive               *bool   `json:"is_active"`
}

type Counselor struct {
	StaffNumber string  `json:"staff_number"`
	Name        *string `json:"name"`
}

type StudentsResponse struct {
	Total    int            `json:"total"`
	SyncedAt string         `json:"synced_at"`
	Items    Array[Student] `json:"items"`
}

type Student struct {
	StudentNumber          string      `json:"student_number"`
	Name                   string      `json:"name"`
	StatusCode             *string     `json:"status_code"`
	EnrollmentStatusCode   *string     `json:"enrollment_status_code"`
	OnCampusFlag           *string     `json:"on_campus_flag"`
	Department             *Department `json:"department"`
	CampusCode             *string     `json:"campus_code"`
	CampusName             *string     `json:"campus_name"`
	MajorCode              *string     `json:"major_code"`
	MajorName              *string     `json:"major_name"`
	Grade                  *string     `json:"grade"`
	ClassCode              *string     `json:"class_code"`
	ClassName              *string     `json:"class_name"`
	StudentCategoryCode    *string     `json:"student_category_code"`
	StudentCategoryName    *string     `json:"student_category_name"`
	StudyYears             *int        `json:"study_years"`
	AdmissionMonth         *string     `json:"admission_month"`
	ExpectedGraduationDate *string     `json:"expected_graduation_date"`
	Counselor              *Counselor  `json:"counselor"`
	Email                  *string     `json:"email"`
	Mobile                 *string     `json:"mobile"`
	SourceUpdatedAt        *string     `json:"source_updated_at"`
}

type StaffResponse struct {
	Total    int                `json:"total"`
	SyncedAt string             `json:"synced_at"`
	Items    Array[StaffMember] `json:"items"`
}

type StaffMember struct {
	StaffNumber     string      `json:"staff_number"`
	Name            string      `json:"name"`
	StatusCode      *string     `json:"status_code"`
	Department      *Department `json:"department"`
	Email           *string     `json:"email"`
	Mobile          *string     `json:"mobile"`
	SourceUpdatedAt *string     `json:"source_updated_at"`
}

type StaffTeachingClassesResponse struct {
	StaffNumber  string               `json:"staff_number"`
	AcademicYear string               `json:"academic_year"`
	Semester     string               `json:"semester"`
	Total        int                  `json:"total"`
	SyncedAt     string               `json:"synced_at"`
	Items        Array[TeachingClass] `json:"items"`
}

type TeachingClass struct {
	TeachingClassNumber string                      `json:"teaching_class_number"`
	TeachingClassName   *string                     `json:"teaching_class_name"`
	PlannedCourseCode   string                      `json:"planned_course_code"`
	CourseCode          string                      `json:"course_code"`
	CourseName          *string                     `json:"course_name"`
	Teachers            Array[TeachingClassTeacher] `json:"teachers"`
	StudentCount        int                         `json:"student_count"`
	SourceUpdatedAt     *string                     `json:"source_updated_at"`
}

type TeachingClassTeacher struct {
	StaffNumber string  `json:"staff_number"`
	Name        *string `json:"name"`
}

type TeachingClassStudentsResponse struct {
	TeachingClassNumber string                      `json:"teaching_class_number"`
	PlannedCourseCode   string                      `json:"planned_course_code"`
	AcademicYear        string                      `json:"academic_year"`
	Semester            string                      `json:"semester"`
	Total               int                         `json:"total"`
	SyncedAt            string                      `json:"synced_at"`
	Items               Array[TeachingClassStudent] `json:"items"`
}

type TeachingClassStudent struct {
	StudentNumber   string  `json:"student_number"`
	Name            *string `json:"name"`
	DepartmentCode  *string `json:"department_code"`
	DepartmentName  *string `json:"department_name"`
	ClassCode       *string `json:"class_code"`
	ClassName       *string `json:"class_name"`
	SourceUpdatedAt *string `json:"source_updated_at"`
}
