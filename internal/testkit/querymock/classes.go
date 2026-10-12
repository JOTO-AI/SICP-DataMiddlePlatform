package querymock

import (
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const sharedClass = `[{"teaching_class_number":"J2026-0001","teaching_class_name":"虚构教学班1","planned_course_code":"000C0001","course_code":"000C0001","course_name":"虚构课程001","teachers":[{"staff_number":"001","name":"虚构教师001"},{"staff_number":"1001","name":"虚构教师002"}],"student_count":3,"source_updated_at":"2026-08-05T07:50:00+08:00"}]`

func Classes(t *testing.T, q Query) {
	t.Helper()
	exact(t, q, contract.StaffTeachingClasses, `{"staff_number":"001","academic_year":"2026-2027","semester":"1","course_code":"000C0001"}`, sharedClass)
	exact(t, q, contract.StaffTeachingClasses, `{"staff_number":"1001","academic_year":"2026-2027","semester":"1","course_code":"000C0001"}`, sharedClass)
	exact(t, q, contract.StaffTeachingClasses, `{"staff_number":"1","academic_year":"2026-2027","semester":"1"}`, `[]`)
	exact(t, q, contract.StaffTeachingClasses, `{"staff_number":"001","academic_year":"2026-2027","semester":"2"}`, `[]`)
	pages(t, q, contract.StaffTeachingClasses, `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`, "teaching_class_number", []string{"J2026-0001", "J2026-0007"})
	pages(t, q, contract.StaffTeachingClasses, `{"staff_number":"1001","academic_year":"2026-2027","semester":"1"}`, "teaching_class_number", []string{"J2026-0001", "J2026-0002", "J2026-0008"})
}
