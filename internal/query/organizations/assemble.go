package organizations

import (
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const Table = "dwd_yxsdwjbsjzlb"
const Root = "10001"

// nullable preserves nonblank source identifiers without normalizing them.
func nullable(row source.SourceRow, field string) (*string, error) {
	value, err := compute.Optional(row, field)
	if err != nil || value == "" {
		return nil, err
	}
	return &value, nil
}

// Department validates only the referenced record, not the surrounding graph.
// A missing record preserves its foreign key and leaves all other fields null.
func Department(code string, row source.SourceRow) (*contract.Department, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	result := &contract.Department{OrganizationCode: code}
	if row == nil {
		return result, nil
	}
	id, err := compute.Required(row, "DWH")
	if err != nil {
		return nil, err
	}
	if id != code {
		return nil, compute.ErrRecords
	}
	name, err := compute.Required(row, "DWMC")
	if err != nil {
		return nil, err
	}
	result.OrganizationName = &name
	if code != Root {
		result.ParentOrganizationCode, err = nullable(row, "LSDWH")
		if err != nil {
			return nil, err
		}
	}
	result.CategoryCode, err = nullable(row, "DWLBM")
	if err != nil {
		return nil, err
	}
	result.CategoryName = compute.OrganizationCategoryName(result.CategoryCode)
	active, err := compute.Optional(row, "DWYXBSM")
	if err != nil {
		return nil, err
	}
	deleted, err := compute.Optional(row, "SFSC")
	if err != nil {
		return nil, err
	}
	value := active == "1" && deleted == "0"
	result.IsActive = &value
	return result, nil
}

// Assemble checks the entire organization graph before exposing any records.
// Iterative visitation is linear and avoids recursion on deep source trees.
func Assemble(rows []source.SourceRow, times compute.Times, check func() error) ([]contract.Organization, error) {
	items := make([]contract.Organization, 0, len(rows))
	positions := make(map[string]int, len(rows))
	for _, row := range rows {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		code, err := compute.Required(row, "DWH")
		if err != nil {
			return nil, err
		}
		if _, exists := positions[code]; exists {
			return nil, compute.ErrRecords
		}
		dep, err := Department(code, row)
		if err != nil {
			return nil, err
		}
		raw, err := compute.Text(row, "ETL_TIME")
		if err != nil {
			return nil, err
		}
		at, err := times.Source(raw)
		if err != nil {
			return nil, err
		}
		positions[code] = len(items)
		items = append(items, contract.Organization{OrganizationCode: code, OrganizationName: *dep.OrganizationName,
			ParentOrganizationCode: dep.ParentOrganizationCode, CategoryCode: dep.CategoryCode, CategoryName: dep.CategoryName,
			IsActive: *dep.IsActive, SourceUpdatedAt: at})
	}
	root, exists := positions[Root]
	if !exists {
		return nil, compute.ErrRecords
	}
	state := make([]uint8, len(items))
	state[root] = 2
	path := make([]int, 0, len(items))
	for start := range items {
		path = path[:0]
		current := start
		for state[current] != 2 {
			if check != nil {
				if err := check(); err != nil {
					return nil, err
				}
			}
			if state[current] == 1 {
				return nil, compute.ErrRecords
			}
			state[current] = 1
			path = append(path, current)
			parent := items[current].ParentOrganizationCode
			if parent == nil {
				return nil, compute.ErrRecords
			}
			next, exists := positions[*parent]
			if !exists {
				return nil, compute.ErrRecords
			}
			current = next
		}
		for _, index := range path {
			state[index] = 2
		}
	}
	return items, nil
}
