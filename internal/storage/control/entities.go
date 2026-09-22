// Package control persists control facts outside replaceable business slots.
package control

import "time"

// Document is JSON stored in control metadata, not a source-business value.
type Document string

type Task struct {
	ID              string     `db:"id,key"`
	IdempotencyKey  string     `db:"idempotency_key"`
	Mode            string     `db:"mode"`
	Status          string     `db:"status"`
	RequestedBy     string     `db:"requested_by"`
	PredecessorID   *string    `db:"predecessor_id"`
	AcceptedAt      time.Time  `db:"accepted_at"`
	StartedAt       *time.Time `db:"started_at"`
	ReadyAt         *time.Time `db:"ready_at"`
	PublishedAt     *time.Time `db:"published_at"`
	CheckedAt       *time.Time `db:"checked_at"`
	FinishedAt      *time.Time `db:"finished_at"`
	OwnerID         *string    `db:"owner_id"`
	OwnerGeneration int64      `db:"owner_generation"`
	Roles           *Document  `db:"roles"`
	Summary         Document   `db:"summary"`
}
type Step struct {
	TaskID     string     `db:"task_id,key"`
	Name       string     `db:"name,key"`
	Status     string     `db:"status"`
	StartedAt  *time.Time `db:"started_at"`
	FinishedAt *time.Time `db:"finished_at"`
	Evidence   *Document  `db:"evidence"`
}
type Checkpoint struct {
	TaskID     string    `db:"task_id,key"`
	Name       string    `db:"name,key"`
	Payload    Document  `db:"payload"`
	RecordedAt time.Time `db:"recorded_at"`
}
type Schedule struct {
	Singleton     bool       `db:"singleton,key"`
	Revision      int64      `db:"revision"`
	LastStartedAt *time.Time `db:"last_started_at"`
	NextDueAt     *time.Time `db:"next_due_at"`
	Deferred      bool       `db:"deferred"`
	CooldownUntil *time.Time `db:"cooldown_until"`
}
type Execution struct {
	Singleton  bool    `db:"singleton,key"`
	TaskID     *string `db:"task_id"`
	OwnerID    *string `db:"owner_id"`
	Generation int64   `db:"generation"`
}
type Bundle struct {
	ID          string     `db:"id,key"`
	Tables      Document   `db:"tables"`
	CreatedAt   time.Time  `db:"created_at"`
	PublishedAt *time.Time `db:"published_at"`
	CheckedAt   *time.Time `db:"checked_at"`
}
type Slot struct {
	Instance      string  `db:"instance,key"`
	Slot          int16   `db:"slot,key"`
	Generation    int64   `db:"generation"`
	BundleID      *string `db:"bundle_id"`
	AdmissionOpen bool    `db:"admission_open"`
	State         string  `db:"state"`
}
type Candidate struct {
	ID              string     `db:"id,key"`
	TaskID          string     `db:"task_id"`
	Instance        string     `db:"instance"`
	Slot            int16      `db:"slot"`
	Generation      int64      `db:"generation"`
	BundleID        string     `db:"bundle_id"`
	OwnerGeneration int64      `db:"owner_generation"`
	State           string     `db:"state"`
	Evidence        *Document  `db:"evidence"`
	ReadyAt         *time.Time `db:"ready_at"`
}
type Progress struct {
	Source         string    `db:"source,key"`
	ConfirmedUntil time.Time `db:"confirmed_until"`
	CheckedAt      time.Time `db:"checked_at"`
	TaskID         string    `db:"task_id"`
}
type Publication struct {
	ID          string     `db:"id,key"`
	Sequence    int64      `db:"sequence,generated"`
	TaskID      string     `db:"task_id"`
	Kind        string     `db:"kind"`
	Expected    *Document  `db:"expected"`
	Target      Document   `db:"target"`
	Status      string     `db:"status"`
	CreatedAt   time.Time  `db:"created_at"`
	AppliedAt   *time.Time `db:"applied_at"`
	ConfirmedAt *time.Time `db:"confirmed_at"`
}
type Notification struct {
	ID            string     `db:"id,key"`
	FailureKey    string     `db:"failure_key"`
	TaskID        string     `db:"task_id"`
	Payload       Document   `db:"payload"`
	Status        string     `db:"status"`
	Attempts      int        `db:"attempts"`
	CreatedAt     time.Time  `db:"created_at"`
	NextAttemptAt *time.Time `db:"next_attempt_at"`
	SentAt        *time.Time `db:"sent_at"`
}
type Reader struct {
	ID            string     `db:"id,key"`
	Instance      string     `db:"instance"`
	Slot          int16      `db:"slot"`
	Generation    int64      `db:"generation"`
	BundleID      string     `db:"bundle_id"`
	ProcessID     string     `db:"process_id"`
	AdmittedAt    time.Time  `db:"admitted_at"`
	HeartbeatAt   *time.Time `db:"heartbeat_at"`
	SQLFinishedAt *time.Time `db:"sql_finished_at"`
	ReleasedAt    *time.Time `db:"released_at"`
}
