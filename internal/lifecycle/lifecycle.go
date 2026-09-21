// Package lifecycle manages ordered initialization and reverse, budgeted release.
package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
)

type Factory struct {
	Name string
	// Open must honor context, and release any partial resource before returning
	// an error. If it returns a resource with an error, the manager releases it.
	Open func(context.Context) (ports.Resource, error)
}
type owned struct {
	name     string
	resource ports.Resource
}
type Manager struct{ resources []owned }
type Result struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// Start validates the whole factory list before opening anything.
func Start(ctx context.Context, factories []Factory, cleanupBudget time.Duration) (*Manager, []Result, error) {
	m := &Manager{}
	names := map[string]bool{}
	for _, f := range factories {
		if f.Name == "" || f.Open == nil || names[f.Name] {
			return nil, nil, fmt.Errorf("invalid dependency factory")
		}
		names[f.Name] = true
	}
	for _, f := range factories {
		if err := ctx.Err(); err != nil {
			return nil, m.Close(cleanupBudget), fmt.Errorf("startup canceled")
		}
		r, err := f.Open(ctx)
		if r != nil {
			m.resources = append(m.resources, owned{f.Name, r})
		}
		if err != nil || r == nil {
			return nil, m.Close(cleanupBudget), fmt.Errorf("dependency %s: initialization failed", f.Name)
		}
	}
	if ctx.Err() != nil {
		return nil, m.Close(cleanupBudget), fmt.Errorf("startup canceled")
	}
	return m, nil, nil
}

func (m *Manager) Add(name string, resource ports.Resource) {
	m.resources = append(m.resources, owned{name, resource})
}

// Close uses one total budget, including dependencies that ignore cancellation.
// Timed-out resources are reported incomplete, never represented as drained.
// The manager is owned by one lifecycle goroutine; Close is idempotent.
func (m *Manager) Close(budget time.Duration) []Result {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	results := make([]Result, 0, len(m.resources))
	for i := len(m.resources) - 1; i >= 0; i-- {
		r := m.resources[i]
		state := "incomplete"
		if ctx.Err() == nil {
			done := make(chan error, 1)
			go func() { done <- r.resource.Close(ctx) }()
			select {
			case err := <-done:
				if err == nil {
					state = "closed"
				} else if ctx.Err() == nil {
					state = "failed"
				}
			case <-ctx.Done():
			}
		}
		results = append(results, Result{Name: r.name, State: state})
	}
	m.resources = nil
	return results
}
