package handlers_test

import (
	"encoding/json"
	"fmt"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestT32_16And17ProxyClassRosterRoundPreservesVersionAndCountEvidence(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			world := newWorld()
			world.tables = classRosterRepo().tables
			query, _ := pipelineSetup(t, world)
			server := httptest.NewServer(query)
			defer server.Close()
			proxy, err := admin.QueryProxy(server.URL, "synthetic-key")
			if err != nil {
				t.Fatal(err)
			}
			request := func(path, body, token string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/console-api/query/api/v1/sicp/"+path, strings.NewReader(body))
				r.Header.Set(snapshot.Header, token)
				w := httptest.NewRecorder()
				proxy.ServeHTTP(w, r)
				return w
			}
			classes := request("staff/teaching-classes/query", teacherClassesBody, "")
			var declared struct {
				Data contract.StaffTeachingClassesResponse
			}
			if json.Unmarshal(classes.Body.Bytes(), &declared) != nil || classes.Code != 200 || len(declared.Data.Items) != 1 {
				t.Fatal(classes.Code, classes.Body.String())
			}
			version := classes.Header().Get("X-SICP-Combination")
			classToken := classes.Header().Get(snapshot.Header)
			if version != pinned.Combination || classToken == "" || classes.Header().Get("X-Request-ID") == "" {
				t.Fatal("proxy lost version or request identity")
			}
			bad := request("teaching-classes/students/query", classRosterBody, classToken)
			if bad.Code != 409 || !strings.Contains(bad.Body.String(), `"data":null`) {
				t.Fatal("cross-operation token accepted", bad.Body.String())
			}
			if changed {
				world.mu.Lock()
				next := pinned
				next.Generation++
				next.Combination = "new-combination"
				world.active = next
				world.readable[next] = true
				world.tables[timetable.RelationsTable] = append(world.tables[timetable.RelationsTable], sourceRow(`{"ID":"new","JXBH":"J01","JHKCH":"C1","XH":"new-student"}`))
				world.mu.Unlock()
			}
			token := ""
			seen := map[string]bool{}
			versionConflict := false
			total := 0
			for page := 1; page <= 4; page++ {
				body := strings.TrimSuffix(classRosterBody, "}") + fmt.Sprintf(`,"page":%d,"page_size":1}`, page)
				got := request("teaching-classes/students/query", body, token)
				var roster struct {
					Data contract.TeachingClassStudentsResponse
				}
				if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &roster) != nil {
					t.Fatal(got.Code, got.Body.String())
				}
				if page == 1 {
					token = got.Header().Get(snapshot.Header)
					if token == "" || token == classToken {
						t.Fatal("operations did not get independent tokens")
					}
				}
				if got.Header().Get(snapshot.Header) != token {
					t.Fatal("roster token changed")
				}
				versionConflict = versionConflict || got.Header().Get("X-SICP-Combination") != version
				total = roster.Data.Total
				for _, student := range roster.Data.Items {
					if seen[student.StudentNumber] {
						t.Fatal("duplicate roster member")
					}
					seen[student.StudentNumber] = true
				}
			}
			countConflict := total != declared.Data.Items[0].StudentCount || len(seen) != declared.Data.Items[0].StudentCount
			if versionConflict != changed || countConflict != changed {
				t.Fatal("round conflict evidence lost", versionConflict, countConflict)
			}
			if !changed && (total != 2 || len(seen) != 2) {
				t.Fatal("incomplete stable roster")
			}
		})
	}
}
