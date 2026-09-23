package publication

import (
	"context"
	"errors"
	"strconv"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

const ActiveKey = "sicp:active"

var ErrUnknown = errors.New("active metadata update outcome unknown")
var ErrConflict = errors.New("active metadata conditional update rejected")

type Commander interface {
	Do(context.Context, string, ...any) (any, error)
}
type Atomic struct{ Client Commander }

// Expected/target are canonical complete metadata, not partial field updates.
// Sequences are decimal strings: length+lexical ordering is exact above 2^53.
const compareAndSwap = `local current = redis.call('GET', KEYS[1])
if current == ARGV[2] then return 'already_applied' end
if ARGV[1] == '' then
  if current then return 'conflict' end
else
  if current ~= ARGV[1] then return 'conflict' end
end
local old = ARGV[3]
local next = ARGV[4]
if #next < #old or (#next == #old and next <= old) then return 'conflict' end
redis.call('SET', KEYS[1], ARGV[2])
return 'applied'`

func (a Atomic) Apply(ctx context.Context, e control.Publication) error {
	if a.Client == nil || e.Status != "pending" {
		return control.ErrEntity
	}
	target, err := control.DecodeActive(e.Target)
	if err != nil {
		return err
	}
	if target.Sequence != e.Sequence || target.EventID != e.ID || target.TaskID != e.TaskID {
		return control.ErrEntity
	}
	canonical, err := control.ActiveDocument(target)
	if err != nil {
		return err
	}
	expected := ""
	oldSeq := int64(0)
	if e.Expected != nil {
		old, decodeErr := control.DecodeActive(*e.Expected)
		if decodeErr != nil {
			return decodeErr
		}
		if old.Sequence >= target.Sequence {
			return ErrConflict
		}
		doc, _ := control.ActiveDocument(old)
		expected = string(doc)
		oldSeq = old.Sequence
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	answer, err := a.Client.Do(ctx, "EVAL", compareAndSwap, 1, ActiveKey, expected, string(canonical), strconv.FormatInt(oldSeq, 10), strconv.FormatInt(target.Sequence, 10))
	if err != nil {
		return errors.Join(ErrUnknown, err)
	}
	var outcome string
	switch value := answer.(type) {
	case string:
		outcome = value
	case []byte:
		outcome = string(value)
	default:
		return ErrUnknown
	}
	switch outcome {
	case "applied", "already_applied":
		return nil
	case "conflict":
		return ErrConflict
	default:
		return ErrUnknown
	}
}

func (a Atomic) Read(ctx context.Context) (control.ActiveMetadata, bool, error) {
	if a.Client == nil {
		return control.ActiveMetadata{}, false, control.ErrEntity
	}
	if err := ctx.Err(); err != nil {
		return control.ActiveMetadata{}, false, err
	}
	answer, err := a.Client.Do(ctx, "GET", ActiveKey)
	if err != nil {
		return control.ActiveMetadata{}, false, err
	}
	if err = ctx.Err(); err != nil {
		return control.ActiveMetadata{}, false, err
	}
	if answer == nil {
		return control.ActiveMetadata{}, false, nil
	}
	var raw string
	switch value := answer.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return control.ActiveMetadata{}, false, ErrUnknown
	}
	metadata, err := control.DecodeActive(control.Document(raw))
	if err != nil {
		return control.ActiveMetadata{}, false, err
	}
	if err = ctx.Err(); err != nil {
		return control.ActiveMetadata{}, false, err
	}
	return metadata, true, nil
}
