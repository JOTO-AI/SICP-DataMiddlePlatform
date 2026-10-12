package compute

import "github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"

func Pagination(req contract.PaginationRequest, defaultSize int) (int, int, error) {
	page, size := 1, defaultSize
	if req.Page != nil {
		page = *req.Page
	}
	if req.PageSize != nil {
		size = *req.PageSize
	}
	if page < 1 || page > 1000000 || size < 1 || size > 1000 {
		return 0, 0, ErrValue
	}
	return page, size, nil
}

// PageBounds expects validated pagination and avoids overflow on 32-bit hosts.
func PageBounds(total, page, size int) (int, int) {
	start := int64(page-1) * int64(size)
	if start >= int64(total) {
		return total, total
	}
	end := start + int64(size)
	if end > int64(total) {
		end = int64(total)
	}
	return int(start), int(end)
}
