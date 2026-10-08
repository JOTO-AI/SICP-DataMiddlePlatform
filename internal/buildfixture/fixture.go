// Package buildfixture generates synthetic capacity data without personnel data
// or a dependency on the production merge/canonicalization implementation.
package buildfixture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const Version = "b01-v1"
const TotalRows = 2592390

type Table struct {
	Name, Key string
	Rows      int
}

func Tables() []Table {
	return []Table{
		{"dwd_bzksbjsjzlb", "BH", 286}, {"dwd_bzkscjzlb", "ID", 1439486},
		{"dwd_bzksgrtxsjzlb", "XH", 34883}, {"dwd_bzksjbsjzlb", "XH", 34609},
		{"dwd_bzkskbsjzlb", "ID", 492191}, {"dwd_bzkskcsjzlb", "KCH", 8160},
		{"dwd_bzksnjzysjzlb", "NJZYDM", 562}, {"dwd_bzkspksjzlb", "PKH", 541958},
		{"dwd_bzksxjjbsjzlb", "XH", 34609}, {"dwd_bzkszyxxsjzlb", "ZYH", 307},
		{"dwd_grtxfszlb", "JGH", 2665}, {"dwd_jzgjcsjzlb", "JGH", 2603},
		{"dwd_xqjbsjzlb", "XQH", 2}, {"dwd_yxsdwjbsjzlb", "DWH", 69},
	}
}

// Small uses the same field/distribution generator with at most 24 rows/table.
// Percent is 0 (initial), 1 (all tables changed) or 100 (complete replacement).
// Epoch/ChangedAt are explicit, so a seed never silently depends on wall time.
type Fixture struct {
	Seed             uint64
	Small            bool
	Percent          int
	OnePerTable      bool
	Epoch, ChangedAt time.Time
}

func (f Fixture) Valid() bool {
	return (f.Percent == 0 || f.Percent == 1 || f.Percent == 100) && (!f.OnePerTable || f.Percent == 1) && !f.Epoch.IsZero() && (f.Percent == 0 || f.ChangedAt.After(f.Epoch))
}
func (f Fixture) Count(t Table) int {
	if f.Small {
		return min(t.Rows, 24)
	}
	return t.Rows
}
func (f Fixture) Changed(t Table) int {
	if f.Percent == 0 {
		return 0
	}
	if f.OnePerTable {
		return 1
	}
	return max(1, (f.Count(t)*f.Percent+99)/100)
}
func id(prefix string, n int) string { return fmt.Sprintf("%s%08d", prefix, n) }
func (f Fixture) count(name string) int {
	for _, t := range Tables() {
		if t.Name == name {
			return f.Count(t)
		}
	}
	return 0
}

func (f Fixture) Row(t Table, i int) (json.RawMessage, error) {
	if !f.Valid() || i < 0 || i >= f.Count(t) {
		return nil, errors.New("invalid synthetic fixture")
	}
	stamp := f.Epoch
	revision := 0
	if i < f.Changed(t) {
		stamp = f.ChangedAt
		revision = 1
	}
	// Seed perturbs non-key content; fixed-width, ascending keys make the
	// reference digest streamable independently of the production external sort.
	n := int((uint64(i) + f.Seed) % 997)
	m := map[string]any{t.Key: id("0", i), "ETL_TIME": stamp.UTC().Format("2006-01-02 15:04:05.000000")}
	students := f.count("dwd_bzksjbsjzlb")
	courses := f.count("dwd_bzkskcsjzlb")
	staff := f.count("dwd_jzgjcsjzlb")
	course := i % courses
	class := i % (courses * 4)
	if f.Small {
		class = i % courses
	}
	switch t.Name {
	case "dwd_bzkscjzlb":
		m["ID"] = fmt.Sprintf("G%023d", i)
		m["XH"] = id("0", i%students)
		m["KCH"] = id("0", course)
		m["XN"] = fmt.Sprintf("%d-%d", 2022+i%4, 2023+i%4)
		m["XQM"] = strconv.Itoa(1 + i%2)
		m["XF"] = json.RawMessage([]string{"3", "3.0", `"3"`, "null"}[i%4])
		m["JD"] = json.Number("3.0")
		m["KCCJ"] = 60 + (n+revision)%40
	case "dwd_bzkspksjzlb":
		m["JHKCH"] = id("0", course)
		m["JXBH"] = id("J", class)
		m["KKXND"] = fmt.Sprintf("%d-%d", 2022+class%4, 2023+class%4)
		m["KKXQM"] = strconv.Itoa(1 + class%2)
		m["XQJ"] = strconv.Itoa(1 + i%7)
		m["SKJC"] = "1,2"
		m["ZC"] = "1,3,5,7"
		m["SKRQ"] = "2026-09-07,2026-09-21"
		m["JSJGH"] = id("0", i%staff) + "," + id("0", (i+1)%staff)
		m["JSXM"] = "合成教师甲,合成教师乙"
		m["JXBMC"] = fmt.Sprintf("合成教学班%04d", class)
		m["JXDD"] = fmt.Sprintf("合成教室%03d", (n+revision)%120)
		m["XDRS"] = 30 + i%50
	case "dwd_bzkskbsjzlb":
		m["XH"] = id("0", i%students)
		m["JHKCH"] = id("0", course)
		m["JXBH"] = id("J", class)
	case "dwd_bzkskcsjzlb":
		m["KCMC"] = fmt.Sprintf("合成课程%04d", i)
		m["KKXY"] = id("0", i%f.count("dwd_yxsdwjbsjzlb"))
		m["XF"] = 3
		m["KCJJ"] = strings.Repeat("合成课程说明", 1+(n+revision)%24)
	case "dwd_bzksjbsjzlb":
		m["XM"] = fmt.Sprintf("合成学生%08d", i)
		m["TC"] = strings.Repeat("甲", n%48)
		m["YWXM"] = nil
	case "dwd_bzksxjjbsjzlb":
		m["YXSH"] = id("0", i%f.count("dwd_yxsdwjbsjzlb"))
		m["SZBH"] = id("0", i%f.count("dwd_bzksbjsjzlb"))
		m["ZYH"] = id("0", i%f.count("dwd_bzkszyxxsjzlb"))
		m["SZNJ"] = "2026"
		m["XZ"] = 4
	case "dwd_bzksgrtxsjzlb", "dwd_grtxfszlb":
		m["YDDH"] = nil
		m["DZXX"] = fmt.Sprintf("synthetic-%08d-r%d@example.invalid", i, revision)
	case "dwd_jzgjcsjzlb":
		m["XM"] = fmt.Sprintf("合成教师%08d", i)
		m["DWH"] = id("0", i%f.count("dwd_yxsdwjbsjzlb"))
		m["TC"] = strings.Repeat("乙", n%64)
	case "dwd_bzksbjsjzlb":
		m["BJMC"] = fmt.Sprintf("合成班级%08d", i)
		m["SSZYH"] = id("0", i%f.count("dwd_bzkszyxxsjzlb"))
		m["SSXQH"] = id("0", i%2)
	case "dwd_bzkszyxxsjzlb":
		m["ZYMC"] = fmt.Sprintf("合成专业%08d", i)
		m["DWH"] = id("0", i%f.count("dwd_yxsdwjbsjzlb"))
		m["XZ"] = 4
	case "dwd_bzksnjzysjzlb":
		m["NJZYMC"] = fmt.Sprintf("合成年级专业%08d", i)
		m["ZYH"] = id("0", i%f.count("dwd_bzkszyxxsjzlb"))
		m["NJ"] = "2026"
	case "dwd_xqjbsjzlb":
		m["XQMC"] = fmt.Sprintf("合成校区%d", i)
		m["XQCX"] = nil
	case "dwd_yxsdwjbsjzlb":
		m["DWMC"] = fmt.Sprintf("合成院系%08d", i)
		m["LSXQH"] = id("0", i%2)
	default:
		return nil, errors.New("unknown synthetic table")
	}
	return json.Marshal(m)
}

type Summary struct {
	Table   string `json:"table"`
	Rows    int    `json:"rows"`
	Bytes   int64  `json:"bytes"`
	Changed int    `json:"changed"`
	SHA256  string `json:"sha256"`
}

// Expected hashes the independent generator's sorted JSON, not merge output.
// It includes all retained rows for incremental inputs, not just the delta.
func (f Fixture) Expected(ctx context.Context, scope string) ([]Summary, error) {
	if !f.Valid() || scope == "" {
		return nil, errors.New("invalid synthetic fixture")
	}
	out := make([]Summary, 0, 14)
	for _, t := range Tables() {
		h := sha256.New()
		header, _ := json.Marshal(struct{ Table, Scope string }{t.Name, scope})
		h.Write(header)
		h.Write([]byte{'\n'})
		s := Summary{Table: t.Name, Rows: f.Count(t), Changed: f.Changed(t)}
		for i := 0; i < s.Rows; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			raw, err := f.Row(t, i)
			if err != nil {
				return nil, err
			}
			h.Write(raw)
			h.Write([]byte{'\n'})
			s.Bytes += int64(len(raw) + 1)
		}
		s.SHA256 = hex.EncodeToString(h.Sum(nil))
		out = append(out, s)
	}
	return out, nil
}

// Handler is used only by isolated tests. Pagination is generated on demand,
// bounded by 1000 records, and obeys the collector's real fixed time window.
func (f Fixture) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		page, e1 := strconv.Atoi(q.Get("page"))
		size, e2 := strconv.Atoi(q.Get("pageSize"))
		if !f.Valid() || token == "" || q.Get("access_token") != token || e1 != nil || e2 != nil || page < 1 || page > 100000 || size < 1 || size > 1000 {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		var table Table
		for _, t := range Tables() {
			if r.URL.Path == "/apizt/openapi/invoke/sjzc/release/"+t.Name {
				table = t
			}
		}
		if table.Name == "" {
			http.NotFound(w, r)
			return
		}
		lo, hi := 0, f.Count(table)
		if q.Get("startTime") != "" || q.Get("endTime") != "" {
			start, e1 := time.Parse("2006-01-02 15:04:05.999999", q.Get("startTime"))
			end, e2 := time.Parse("2006-01-02 15:04:05.999999", q.Get("endTime"))
			if e1 != nil || e2 != nil || !start.Before(end) {
				http.Error(w, "invalid fixture window", 400)
				return
			}
			inside := func(at time.Time) bool { return !at.Before(start) && at.Before(end) }
			old, newer := inside(f.Epoch), f.Percent > 0 && inside(f.ChangedAt)
			if !old && !newer {
				lo, hi = 0, 0
			} else if !old {
				hi = f.Changed(table)
			} else if !newer {
				lo = f.Changed(table)
			}
		}
		total := hi - lo
		begin := min(lo+(page-1)*size, hi)
		finish := min(begin+size, hi)
		rows := make([]json.RawMessage, 0, finish-begin)
		for i := begin; i < finish; i++ {
			raw, err := f.Row(table, i)
			if err != nil {
				http.Error(w, "fixture generation failed", 500)
				return
			}
			rows = append(rows, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "ok", "data": map[string]any{"total": total, "list": rows}})
	})
}
