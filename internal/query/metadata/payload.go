package metadata

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

// Payload accepts only a successful typed response. Synchronization time is
// deliberately absent from the cache; source_updated_at remains business data.
func Payload(value any) ([]byte, error) {
	switch value.(type) {
	case contract.GradesResponse, contract.StudentTimetableResponse, contract.StaffTimetableResponse,
		contract.OrganizationsResponse, contract.StudentsResponse, contract.StaffResponse,
		contract.StaffTeachingClassesResponse, contract.TeachingClassStudentsResponse:
	default:
		return nil, ErrMetadata
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	delete(fields, "synced_at")
	return json.Marshal(fields)
}

// Compose applies freshly read metadata to cached or freshly loaded business
// data alike. A stale envelope or a payload for another operation is rejected.
func Compose(op contract.Operation, payload []byte, at time.Time, times compute.Times) (any, error) {
	formatted, err := times.Instant(at)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(payload, &fields); err != nil {
		return nil, err
	}
	var result any
	var required []string
	switch op {
	case contract.Organizations:
		result = &contract.OrganizationsResponse{SyncedAt: *formatted}
		required = []string{"total", "items"}
	case contract.Students:
		result = &contract.StudentsResponse{SyncedAt: *formatted}
		required = []string{"total", "items"}
	case contract.Staff:
		result = &contract.StaffResponse{SyncedAt: *formatted}
		required = []string{"total", "items"}
	case contract.StaffTeachingClasses:
		result = &contract.StaffTeachingClassesResponse{SyncedAt: *formatted}
		required = []string{"staff_number", "academic_year", "semester", "total", "items"}
	case contract.TeachingClassStudents:
		result = &contract.TeachingClassStudentsResponse{SyncedAt: *formatted}
		required = []string{"teaching_class_number", "planned_course_code", "academic_year", "semester", "total", "items"}
	case contract.StudentGrades, contract.BatchGrades:
		result = &contract.GradesResponse{SyncedAt: *formatted}
		required = []string{"total", "items"}
	case contract.StudentTimetable:
		result = &contract.StudentTimetableResponse{SyncedAt: *formatted}
		required = []string{"student_number", "academic_year", "semester", "items"}
	case contract.StaffTimetable:
		result = &contract.StaffTimetableResponse{SyncedAt: *formatted}
		required = []string{"staff_number", "academic_year", "semester", "items"}
	default:
		return nil, ErrMetadata
	}
	if len(fields) != len(required) {
		return nil, ErrMetadata
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrMetadata
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(result); err != nil {
		return nil, err
	}
	return result, nil
}
