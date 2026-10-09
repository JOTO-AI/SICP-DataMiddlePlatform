package timetable

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Calendar struct {
	Weekday  int
	Sections []int
	Weeks    []int
	Dates    []string
}

// ParseCalendar uses only explicit schedule fields, never start/end weeks or
// a guessed semester calendar. Any invalid field invalidates the whole result.
func ParseCalendar(row source.SourceRow) (Calendar, error) {
	var out Calendar
	weekday, err := compute.Required(row, "XQJ")
	if err != nil {
		return Calendar{}, err
	}
	weekdays := map[string]int{"1": 1, "2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7,
		"星期一": 1, "星期二": 2, "星期三": 3, "星期四": 4, "星期五": 5, "星期六": 6, "星期日": 7, "星期天": 7}
	out.Weekday = weekdays[strings.TrimSpace(weekday)]
	if out.Weekday == 0 {
		return Calendar{}, compute.ErrValue
	}
	if out.Sections, err = positiveList(row, "SKJC"); err != nil {
		return Calendar{}, err
	}
	if out.Weeks, err = positiveList(row, "ZC"); err != nil {
		return Calendar{}, err
	}
	dateValue, err := compute.Optional(row, "SKRQ")
	if err != nil {
		return Calendar{}, err
	}
	items := []string{}
	out.Dates = []string{}
	if dateValue != "" {
		items, err = tokens(row, "SKRQ")
	}
	if err != nil {
		return Calendar{}, err
	}
	seen := map[string]bool{}
	for _, item := range items {
		layout := "2006-01-02"
		if len(item) == 8 {
			layout = "20060102"
		}
		date, err := time.Parse(layout, item)
		if err != nil || date.Year() < 1 || date.Format(layout) != item {
			return Calendar{}, compute.ErrValue
		}
		value := date.Format("2006-01-02")
		if !seen[value] {
			out.Dates = append(out.Dates, value)
			seen[value] = true
		}
	}
	sort.Strings(out.Dates)
	return out, nil
}

func tokens(row source.SourceRow, field string) ([]string, error) {
	value, err := compute.Required(row, field)
	if err != nil || len(value) > 65536 {
		return nil, compute.ErrValue
	}
	items := strings.Split(value, ",")
	if len(items) > 4096 {
		return nil, compute.ErrValue
	}
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
		if items[i] == "" {
			return nil, compute.ErrValue
		}
	}
	return items, nil
}

func positiveList(row source.SourceRow, field string) ([]int, error) {
	items, err := tokens(row, field)
	if err != nil {
		return nil, err
	}
	out := []int{}
	seen := map[int]bool{}
	for _, item := range items {
		for _, ch := range item {
			if ch < '0' || ch > '9' {
				return nil, compute.ErrValue
			}
		}
		value, err := strconv.ParseInt(item, 10, 32)
		if err != nil || value < 1 {
			return nil, compute.ErrValue
		}
		n := int(value)
		if !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	sort.Ints(out)
	return out, nil
}

type SectionTime struct{ Start, End string }

// SectionHours returns only published hours. Unknown positive sections can
// remain in the API's sections array without inventing additional JSON fields.
func SectionHours(section int) (SectionTime, bool) {
	hours := [...]SectionTime{{"08:20", "08:59"}, {"09:00", "09:40"}, {"09:50", "10:30"},
		{"10:35", "11:10"}, {"11:15", "11:55"}, {"13:10", "13:49"}, {"13:50", "14:30"},
		{"14:40", "15:19"}, {"15:20", "16:00"}, {"16:10", "16:49"}, {"16:50", "17:30"},
		{"17:35", "18:15"}, {"19:00", "19:40"}}
	if section < 1 || section > len(hours) {
		return SectionTime{}, false
	}
	return hours[section-1], true
}
