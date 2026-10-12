package timetable

import (
	"math"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const (
	SchedulesTable = "dwd_bzkspksjzlb"
	RelationsTable = "dwd_bzkskbsjzlb"
	CoursesTable   = "dwd_bzkskcsjzlb"
)

type Assembler struct {
	courses compute.Index
	times   compute.Times
}

func NewAssembler(courses []source.SourceRow, times compute.Times) (Assembler, error) {
	index, err := compute.IndexRows(courses, "KCH")
	if err != nil {
		return Assembler{}, err
	}
	return Assembler{courses: index, times: times}, nil
}

// Schedule joins the current course by the complete planned course code.
// Membership rows contribute provenance only; callers establish ownership.
func (a Assembler) Schedule(row source.SourceRow, membership ...source.SourceRow) (out compute.Schedule, err error) {
	defer func() {
		if err != nil {
			out = compute.Schedule{}
		}
	}()
	if out.PKH, err = compute.Required(row, "PKH"); err != nil {
		return
	}
	e := &out.Entry
	if e.PlannedCourseCode, err = compute.Optional(row, "JHKCH"); err != nil {
		return
	}
	e.CourseCode = e.PlannedCourseCode
	if e.TeachingClassNumber, err = compute.Optional(row, "JXBH"); err != nil {
		return
	}
	calendar, err := ParseCalendar(row)
	if err != nil {
		return out, err
	}
	e.Weekday, e.Sections, e.Weeks, e.Dates = calendar.Weekday, calendar.Sections, calendar.Weeks, calendar.Dates
	teachers, err := ParseTeachers(row)
	if err != nil {
		return out, err
	}
	e.TeacherStaffNumbers, e.TeacherNames = teachers.StaffNumbers, teachers.Names
	for _, field := range []struct {
		key string
		dst **string
	}{
		{"JXBMC", &e.TeachingClassName}, {"SKJSH", &e.RoomCode}, {"JXDD", &e.Location},
	} {
		if *field.dst, err = compute.Text(row, field.key); err != nil {
			return
		}
	}
	count, err := compute.Number(row, "XDRS", false)
	if err != nil {
		return out, err
	}
	if count != nil {
		if *count < 0 || math.Trunc(*count) != *count || *count > 9007199254740991 || *count >= float64(int(^uint(0)>>1)) {
			return out, compute.ErrValue
		}
		n := int(*count)
		e.EnrolledCount = &n
	}
	course := a.courses[e.CourseCode]
	if e.CourseName, err = compute.Text(course, "KCMC"); err != nil {
		return
	}
	if e.Credits, err = compute.Number(course, "XF", false); err != nil {
		return
	}
	var times []*string
	participants := append([]source.SourceRow{row, course}, membership...)
	for _, participant := range participants {
		var at *string
		if at, err = compute.Text(participant, "ETL_TIME"); err != nil {
			return
		}
		times = append(times, at)
	}
	e.SourceUpdatedAt, err = a.times.Latest(times...)
	return
}
