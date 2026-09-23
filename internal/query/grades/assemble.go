package grades

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const (
	GradesTable      = "dwd_bzkscjzlb"
	StudentsTable    = "dwd_bzksjbsjzlb"
	EnrollmentsTable = "dwd_bzksxjjbsjzlb"
	CoursesTable     = "dwd_bzkskcsjzlb"
	DepartmentsTable = "dwd_yxsdwjbsjzlb"
	ClassesTable     = "dwd_bzksbjsjzlb"
)

// Related contains the current published records, including the enrollment
// winners already resolved by P04. Duplicate keys are a consistency error.
type Related struct{ Students, Enrollments, Courses, Departments, Classes []source.SourceRow }
type Assembler struct {
	students, enrollments, courses, departments, classes compute.Index
	times                                                compute.Times
}

func NewAssembler(data Related, times compute.Times) (Assembler, error) {
	a := Assembler{times: times}
	for _, entry := range []struct {
		rows []source.SourceRow
		key  string
		dst  *compute.Index
	}{
		{data.Students, "XH", &a.students}, {data.Enrollments, "XH", &a.enrollments}, {data.Courses, "KCH", &a.courses}, {data.Departments, "DWH", &a.departments}, {data.Classes, "BH", &a.classes},
	} {
		index, err := compute.IndexRows(entry.rows, entry.key)
		if err != nil {
			return Assembler{}, err
		}
		*entry.dst = index
	}
	return a, nil
}

func (a Assembler) Grade(row source.SourceRow) (result contract.Grade, err error) {
	// Clear all partial fields on failure, including errors in late associations.
	defer func() {
		if err != nil {
			result = contract.Grade{}
		}
	}()
	if result.GradeID, err = compute.Required(row, "ID"); err != nil {
		return
	}
	if result.Student.StudentNumber, err = compute.Required(row, "XH"); err != nil {
		return
	}
	if result.AcademicYear, err = compute.Required(row, "XN"); err != nil {
		return
	}
	if result.Semester, err = compute.Required(row, "XQM"); err != nil {
		return
	}
	if result.Course.CourseCode, err = compute.Required(row, "KCH"); err != nil {
		return
	}
	if result.Score, err = compute.Number(row, "KCCJ", true); err != nil {
		return
	}
	if result.GradePoint, err = compute.Number(row, "JD", false); err != nil {
		return
	}
	if result.Credits, err = compute.Number(row, "XF", false); err != nil {
		return
	}
	student := a.students[result.Student.StudentNumber]
	enrollment := a.enrollments[result.Student.StudentNumber]
	if result.Student.Name, err = compute.Text(student, "XM"); err != nil {
		return
	}
	if result.Student.DepartmentCode, err = compute.Text(enrollment, "YXSH"); err != nil {
		return
	}
	if result.Student.ClassCode, err = compute.Text(enrollment, "SZBH"); err != nil {
		return
	}
	studentDept := a.departments[textValue(result.Student.DepartmentCode)]
	class := a.classes[textValue(result.Student.ClassCode)]
	if result.Student.DepartmentName, err = compute.Text(studentDept, "DWMC"); err != nil {
		return
	}
	if result.Student.ClassName, err = compute.Text(class, "BJMC"); err != nil {
		return
	}
	course := a.courses[result.Course.CourseCode]
	var deptCode *string
	if deptCode, err = compute.Text(course, "KKXY"); err != nil {
		return
	}
	courseDept := a.departments[textValue(deptCode)]
	if deptCode != nil && *deptCode != "" {
		result.Course.Department = &contract.CourseDepartment{OrganizationCode: *deptCode}
		if result.Course.Department.OrganizationName, err = compute.Text(courseDept, "DWMC"); err != nil {
			return
		}
	}
	for _, entry := range []struct {
		key string
		dst **string
	}{
		{"KCMC", &result.Course.CourseName}, {"KCSXM", &result.Course.AttributeCode}, {"KCXZM", &result.Course.NatureCode}, {"KCLXM", &result.Course.TypeCode},
		{"SFCXKC", &result.Course.RetakeFlag}, {"CXKCH", &result.Course.RetakeCourseCode}, {"CXKCMC", &result.Course.RetakeCourseName},
	} {
		if *entry.dst, err = compute.Text(course, entry.key); err != nil {
			return
		}
	}
	// No authoritative complete code dictionary is present; raw codes remain,
	// and the optional names stay null instead of inferring from examples.
	if result.Course.TotalHours, err = compute.Number(course, "ZXS", false); err != nil {
		return
	}
	var times []*string
	for _, participating := range []source.SourceRow{row, student, enrollment, studentDept, class, course, courseDept} {
		var at *string
		if at, err = compute.Text(participating, "ETL_TIME"); err != nil {
			return
		}
		times = append(times, at)
	}
	result.SourceUpdatedAt, err = a.times.Latest(times...)
	return
}

func textValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
