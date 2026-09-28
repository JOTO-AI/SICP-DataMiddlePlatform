package control

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// Decimal strings preserve all BIGINT sequence/fence bits through Redis Lua and
// JSON clients; they are never compared as IEEE-754 numbers.
type ActiveMetadata struct {
	Sequence        int64        `json:"sequence,string"`
	EventID         string       `json:"event_id"`
	TaskID          string       `json:"task_id"`
	OwnerGeneration int64        `json:"owner_generation,string"`
	Position        SlotIdentity `json:"position"`
	// LegacyCodeVersion preserves old publication documents, without controlling admission.
	LegacyCodeVersion string `json:"code_version,omitempty"`
}

func (a ActiveMetadata) Validate() error {
	if a.Sequence < 1 || a.EventID == "" || len(a.EventID) > 128 || a.TaskID == "" || len(a.TaskID) > 128 || a.OwnerGeneration < 1 || !a.Position.valid() || a.Position.Generation < 1 {
		return ErrEntity
	}
	return nil
}
func DecodeActive(raw Document) (ActiveMetadata, error) {
	if len(raw) > 4096 {
		return ActiveMetadata{}, ErrEntity
	}
	var a ActiveMetadata
	d := json.NewDecoder(bytes.NewReader([]byte(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return ActiveMetadata{}, ErrEntity
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ActiveMetadata{}, ErrEntity
	}
	if err := a.Validate(); err != nil {
		return ActiveMetadata{}, err
	}
	return a, nil
}
func ActiveDocument(a ActiveMetadata) (Document, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(a)
	if len(raw) > 4096 {
		return "", ErrEntity
	}
	return Document(raw), err
}

type PublicationIntent struct {
	ID, Kind  string
	Expected  *ActiveMetadata
	Target    SlotIdentity
	CreatedAt time.Time
}

func (i PublicationIntent) validate() error {
	if strings.TrimSpace(i.ID) == "" || len(i.ID) > 128 || !i.Target.valid() || i.Target.Generation < 1 || i.CreatedAt.IsZero() {
		return ErrEntity
	}
	if i.Expected != nil && i.Expected.Validate() != nil {
		return ErrEntity
	}
	switch i.Kind {
	case "route_shift":
		if i.Expected == nil || i.Expected.Position.BundleID != i.Target.BundleID || i.Expected.Position.Position.Instance == i.Target.Position.Instance {
			return ErrEntity
		}
	case "bundle_publish":
		if i.Expected != nil && (i.Expected.Position.BundleID == i.Target.BundleID || i.Expected.Position.Position.Instance != i.Target.Position.Instance || i.Expected.Position.Position == i.Target.Position) {
			return ErrEntity
		}
	default:
		return ErrEntity
	}
	return nil
}

// EnqueuePublication is the publication pipeline's intent write path. No network
// operation occurs until its transaction has acknowledged commit. Replays are
// identified by the immutable event ID, including after an ambiguous commit.
func (r Repository) EnqueuePublication(ctx context.Context, intent PublicationIntent) (Publication, bool, error) {
	if r.owner == nil {
		return Publication{}, false, ErrOwnership
	}
	if err := intent.validate(); err != nil {
		return Publication{}, false, err
	}
	intent.CreatedAt = intent.CreatedAt.UTC().Truncate(time.Microsecond)
	var expected *Document
	if intent.Expected != nil {
		doc, err := ActiveDocument(*intent.Expected)
		if err != nil {
			return Publication{}, false, err
		}
		expected = &doc
	}
	var result Publication
	created := false
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE id=$1)", intent.ID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			d, _ := describe(&Publication{})
			loaded, err := readEntity(ctx, tx, d, `"id"=$1`, []any{intent.ID}, true)
			if err != nil {
				return err
			}
			result = loaded.Interface().(Publication)
			target, err := DecodeActive(result.Target)
			if err != nil {
				return err
			}
			if target.Sequence != result.Sequence || target.EventID != result.ID || target.TaskID != result.TaskID {
				return ErrEntity
			}
			if result.TaskID != r.owner.TaskID || result.Kind != intent.Kind || target.Position != intent.Target || !result.CreatedAt.Equal(intent.CreatedAt) || !sameActiveDocument(result.Expected, expected) {
				return ErrIdempotency
			}
			return nil
		}
		var pending bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		var confirmed *string
		if err := tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&confirmed); err != nil {
			return err
		}
		var current *Document
		if confirmed != nil {
			doc := Document(*confirmed)
			current = &doc
		}
		if !sameActiveDocument(current, expected) {
			return ErrIdempotency
		}
		if err := publicationTarget(ctx, tx, intent, r.owner.TaskID); err != nil {
			return err
		}
		target := ActiveMetadata{Sequence: 1, EventID: intent.ID, TaskID: r.owner.TaskID, OwnerGeneration: r.owner.Generation, Position: intent.Target}
		initial, err := ActiveDocument(target)
		if err != nil {
			return err
		}
		result = Publication{ID: intent.ID, TaskID: r.owner.TaskID, Kind: intent.Kind, Expected: expected, Target: initial, Status: "pending", CreatedAt: intent.CreatedAt}
		var expectedValue any
		if expected != nil {
			expectedValue = string(*expected)
		}
		if err := tx.QueryRow(ctx, "INSERT INTO control.publication_outbox (id,task_id,kind,expected,target,status,created_at) VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,'pending',$6) RETURNING sequence", result.ID, result.TaskID, result.Kind, expectedValue, string(initial), result.CreatedAt).Scan(&result.Sequence); err != nil {
			return err
		}
		if result.Sequence < 1 || intent.Expected != nil && result.Sequence <= intent.Expected.Sequence {
			return ErrEntity
		}
		target.Sequence = result.Sequence
		result.Target, err = ActiveDocument(target)
		if err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.publication_outbox SET target=$2::jsonb WHERE id=$1 AND status='pending'", result.ID, string(result.Target))); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Publication{}, false, err
	}
	return result, created, nil
}
func sameActiveDocument(a, b *Document) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, e1 := DecodeActive(*a)
	right, e2 := DecodeActive(*b)
	return e1 == nil && e2 == nil && left == right
}
func publicationTarget(ctx context.Context, tx storage.Transaction, i PublicationIntent, task string) error {
	if err := lockSlot(ctx, tx, i.Target.Position); err != nil {
		return err
	}
	slot, err := loadSlot(ctx, tx, i.Target)
	if err != nil {
		return err
	}
	if slot.State != "ready" {
		return ErrBusy
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.candidates WHERE instance=$1 AND slot=$2 AND generation=$3 AND bundle_id=$4 AND state='ready' AND ready_at IS NOT NULL AND evidence IS NOT NULL)", slot.Instance, slot.Slot, i.Target.Generation, i.Target.BundleID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return ErrBusy
	}
	if i.Kind == "route_shift" {
		if !slot.AdmissionOpen {
			return ErrBusy
		}
		var published bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.bundles WHERE id=$1 AND published_at IS NOT NULL)", i.Target.BundleID).Scan(&published); err != nil {
			return err
		}
		if !published {
			return ErrBusy
		}
		return nil
	}
	// A single ready copy can never authorize initial or later bundle publication.
	var unpublished bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.bundles WHERE id=$1 AND published_at IS NULL)", i.Target.BundleID).Scan(&unpublished); err != nil {
		return err
	}
	if !unpublished {
		return ErrBusy
	}
	var copies int
	err = tx.QueryRow(ctx, "SELECT COUNT(DISTINCT c.instance) FROM control.candidates c JOIN control.slots s ON s.instance=c.instance AND s.slot=c.slot AND s.generation=c.generation AND s.bundle_id=c.bundle_id WHERE c.bundle_id=$1 AND c.task_id=$2 AND c.state='ready' AND c.ready_at IS NOT NULL AND c.evidence IS NOT NULL AND s.state='ready'", i.Target.BundleID, task).Scan(&copies)
	if err != nil {
		return err
	}
	if copies != 2 || slot.AdmissionOpen {
		return ErrBusy
	}
	return nil
}
