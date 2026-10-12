package snapshot

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

const Header = "X-Query-Snapshot"
const DefaultRetention = 5 * time.Minute

var ErrSnapshot = errors.New("snapshot unavailable or incompatible")

type Store interface {
	PutSnapshot(context.Context, control.Snapshot) error
	GetSnapshot(context.Context, string) (control.Snapshot, error)
}

// Routes confirms readable positions, never candidates or unresolved pointers.
type Routes interface {
	Active(context.Context) (read.Handle, error)
	ForCombination(context.Context, string) (read.Handle, error)
}
type Manager struct {
	Store     Store
	Routes    Routes
	Clock     ports.Clock
	Random    ports.Random
	Retention time.Duration
}
type Selection struct {
	Handle    read.Handle
	Token     string
	ExpiresAt time.Time
}

func binding(bundle string, op contract.Operation, input any) (string, error) {
	page := 1
	switch req := input.(type) {
	case contract.OrganizationsRequest:
		if op != contract.Organizations {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	case contract.StudentsRequest:
		if op != contract.Students {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	case contract.StaffRequest:
		if op != contract.Staff {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	case contract.StaffTeachingClassesRequest:
		if op != contract.StaffTeachingClasses {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	case contract.TeachingClassStudentsRequest:
		if op != contract.TeachingClassStudents {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	case contract.StudentGradesRequest:
		if op != contract.StudentGrades {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	case contract.BatchGradesRequest:
		if op != contract.BatchGrades {
			return "", ErrSnapshot
		}
		req.Page = &page
		return cache.Key(bundle, op, req)
	default:
		return "", ErrSnapshot
	}
}
func validToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (m Manager) Select(ctx context.Context, op contract.Operation, input any, token string) (Selection, error) {
	if m.Store == nil || m.Routes == nil || m.Clock == nil || m.Random == nil {
		return Selection{}, ErrSnapshot
	}
	retention := m.Retention
	if retention == 0 {
		retention = DefaultRetention
	}
	if retention < time.Minute || retention > time.Hour {
		return Selection{}, ErrSnapshot
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	now := m.Clock.Now()
	if token != "" {
		if !validToken(token) {
			return Selection{}, ErrSnapshot
		}
		s, err := m.Store.GetSnapshot(ctx, token)
		if err != nil {
			if errors.Is(err, control.ErrMissing) {
				return Selection{}, ErrSnapshot
			}
			return Selection{}, err
		}
		if s.Token != token || s.Operation != string(op) || now.Before(s.CreatedAt) || !now.Before(s.ExpiresAt) || s.ExpiresAt.Sub(s.CreatedAt) > time.Hour {
			return Selection{}, ErrSnapshot
		}
		conditions, err := binding(s.BundleID, op, input)
		if err != nil || conditions != s.Conditions {
			return Selection{}, ErrSnapshot
		}
		h, err := m.Routes.ForCombination(ctx, s.BundleID)
		if err != nil && !errors.Is(err, read.ErrHandle) {
			return Selection{}, err
		}
		if err != nil || h.Validate() != nil || h.Combination != s.BundleID {
			return Selection{}, ErrSnapshot
		}
		return Selection{Handle: h, Token: token, ExpiresAt: s.ExpiresAt}, nil
	}
	h, err := m.Routes.Active(ctx)
	if err != nil {
		return Selection{}, err
	}
	if h.Validate() != nil {
		return Selection{}, ErrSnapshot
	}
	conditions, err := binding(h.Combination, op, input)
	if err != nil {
		return Selection{}, err
	}
	var random [32]byte
	if _, err = io.ReadFull(m.Random, random[:]); err != nil {
		return Selection{}, err
	}
	token = hex.EncodeToString(random[:])
	s := control.Snapshot{Token: token, BundleID: h.Combination, Operation: string(op), Conditions: conditions, CreatedAt: now, ExpiresAt: now.Add(retention)}
	if err = m.Store.PutSnapshot(ctx, s); err != nil {
		return Selection{}, err
	}
	return Selection{Handle: h, Token: token, ExpiresAt: s.ExpiresAt}, nil
}
