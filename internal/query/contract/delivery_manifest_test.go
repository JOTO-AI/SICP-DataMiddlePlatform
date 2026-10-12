package contract_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type deliveryManifest struct {
	Operations []struct {
		Operation       contract.Operation
		Path            string
		DefaultPageSize *int `json:"default_page_size"`
		Snapshot        bool
		Dependencies    []string
	}
	Migrations []string `json:"business_migrations"`
	Capacity   map[string]json.RawMessage
	Rollback   struct {
		KeepIndexes   bool `json:"keep_additive_indexes"`
		KeepLedger    bool `json:"keep_migration_ledger"`
		PreviousImage bool `json:"previous_image_required"`
		NewOperations bool `json:"old_image_serves_new_operations"`
	}
}

func loadDelivery(t *testing.T) deliveryManifest {
	t.Helper()
	raw, err := os.ReadFile("../../../development_docs/examples/phase2_delivery.json")
	if err != nil {
		t.Fatal(err)
	}
	var result deliveryManifest
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestT25_04PublishedOperationsDependenciesAndPageDefaults(t *testing.T) {
	m := loadDelivery(t)
	if len(m.Operations) != 9 {
		t.Fatal("nine operations required")
	}
	actual := map[contract.Operation]string{}
	for _, d := range contract.Definitions() {
		actual[d.Operation] = d.Path
	}
	for _, entry := range m.Operations {
		if actual[entry.Operation] != entry.Path || contract.SupportsSnapshot(entry.Operation) != entry.Snapshot {
			t.Fatal("route or snapshot contract differs")
		}
		delete(actual, entry.Operation)
		deps, err := metadata.Dependencies(entry.Operation)
		if err != nil || !reflect.DeepEqual(deps, entry.Dependencies) {
			t.Fatal("dependency contract differs", entry.Operation)
		}
		body := `{}`
		switch entry.Operation {
		case contract.StaffTeachingClasses:
			body = `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}`
		case contract.TeachingClassStudents:
			body = `{"teaching_class_number":"J001","planned_course_code":"C001","academic_year":"2026-2027","semester":"1"}`
		case contract.StudentGrades:
			body = `{"student_number":"S001","academic_year":"all"}`
		case contract.BatchGrades:
			body = `{"student_numbers":["S001"],"academic_year":"all"}`
		case contract.StudentTimetable:
			body = `{"student_number":"S001","academic_year":"2026-2027","semester":"1"}`
		case contract.StaffTimetable:
			body = `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}`
		}
		req, err := transport.Parse(entry.Operation, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(req)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if entry.DefaultPageSize != nil {
			var size int
			if json.Unmarshal(fields["page_size"], &size) != nil || size != *entry.DefaultPageSize {
				t.Fatal("default page size differs", entry.Operation)
			}
		}
	}
	if len(actual) != 0 {
		t.Fatal("undocumented operations")
	}
}

func TestT25_05UpgradeRollbackAndCapacityConfiguration(t *testing.T) {
	m := loadDelivery(t)
	plan, err := migrate.Catalog(migrate.Business)
	if err != nil {
		t.Fatal(err)
	}
	versions := []string{}
	for _, entry := range plan {
		versions = append(versions, entry.Version)
	}
	if !reflect.DeepEqual(versions, m.Migrations) || !m.Rollback.KeepIndexes || !m.Rollback.KeepLedger || !m.Rollback.PreviousImage || m.Rollback.NewOperations {
		t.Fatal("upgrade/rollback contract differs")
	}
	values := testkit.RuntimeValues("/data")
	for key, raw := range m.Capacity {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			t.Fatal("invalid capacity")
		}
		if text, ok := value.(string); ok {
			values[key] = text
		} else {
			values[key] = string(raw)
		}
	}
	c, err := config.LoadRuntime(config.Query, func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil || c.QueryMaxExecutions != 8 || c.QueryMaxQueue != 64 || c.QueryExecutionTimeout != 30*time.Second || c.QueryDBBusinessMaxConns != 16 || c.QueryDBControlMaxConns != 16 || c.QueryDBLogMaxConns != 16 {
		t.Fatal("delivery config differs", err)
	}
	for _, entry := range []struct{ key, value string }{{"QUERY_MAX_EXECUTIONS", "0"}, {"QUERY_MAX_QUEUE", "-1"}, {"QUERY_EXECUTION_TIMEOUT", "0s"}, {"QUERY_DB_BUSINESS_MAX_CONNS", "1001"}} {
		previous := values[entry.key]
		values[entry.key] = entry.value
		if _, err := config.LoadRuntime(config.Query, func(k string) (string, bool) { v, ok := values[k]; return v, ok }); err == nil {
			t.Fatal("invalid upgrade config accepted", entry.key)
		}
		values[entry.key] = previous
	}
}
