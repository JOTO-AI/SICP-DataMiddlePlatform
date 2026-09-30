package ingest

import (
	"context"
	"sync"
)

const MaxPageConcurrency = 8

type fetchedPage struct {
	result PageResult
	err    error
}

func (c *Coordinator) fetchWindow(ctx context.Context, input PageRequest, task *TaskBudget, count int) ([]fetchedPage, int) {
	pages := make([]fetchedPage, count)
	if count == 1 {
		pages[0].result, pages[0].err = c.FetchPage(ctx, input, task)
		if pages[0].err != nil {
			return pages, 0
		}
		return pages, -1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	failed := -1
	var first sync.Once
	var workers sync.WaitGroup
	for i := range pages {
		workers.Go(func() {
			request := input
			request.Page += i
			pages[i].result, pages[i].err = c.FetchPage(ctx, request, task)
			if pages[i].err != nil {
				first.Do(func() { failed = i; cancel() })
			}
		})
	}
	// Never release the task/volume owner while any upstream call is in flight.
	workers.Wait()
	return pages, failed
}
