package read

import "context"

type FixedAdmission interface {
	AcquireFixed(context.Context, Handle) (RequestLease, error)
}

type fixedAdmission struct {
	admission FixedAdmission
	h         Handle
}

func (a fixedAdmission) Acquire(ctx context.Context, _ string) (RequestLease, error) {
	return a.admission.AcquireFixed(ctx, a.h)
}

// ExecuteFixed owns a lease for the actual independent cache execution, rather
// than borrowing the first HTTP waiter's lifetime or resolving a route twice.
func ExecuteFixed(ctx context.Context, admission FixedAdmission, h Handle, run func(context.Context, Handle) (any, error)) (any, error) {
	if admission == nil || h.Validate() != nil || run == nil {
		return nil, ErrHandle
	}
	return Execute(ctx, fixedAdmission{admission, h}, "", func(ctx context.Context, actual Handle) (any, error) {
		if actual != h {
			return nil, ErrHandle
		}
		return run(ctx, actual)
	})
}
