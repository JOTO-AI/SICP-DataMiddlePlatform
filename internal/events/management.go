package events

type Purpose string

const (
	SyncPurpose Purpose = "sync"
	TestPurpose Purpose = "test"
)

// Empty purpose is accepted only for existing lifecycle producers and restored
// legacy payloads. New call/attempt spans always carry an explicit purpose.
func (i Identity) validPurpose() bool {
	switch i.Purpose {
	case "", SyncPurpose:
		return i.TestRunID == ""
	case TestPurpose:
		return i.TestRunID != "" && validID(i.TestRunID) && i.TaskID == "" && i.StepID == ""
	default:
		return false
	}
}

// AuditIdentity accepts trusted internal IDs, which must pass the sanitizer
// before any durable sink. Action codes cannot carry request data.
type AuditIdentity struct {
	ActorID  string `json:"actor_id"`
	Action   string `json:"action"`
	ObjectID string `json:"object_id"`
}

func (AuditIdentity) String() string     { return "[audit identity redacted]" }
func (a AuditIdentity) GoString() string { return a.String() }
func (a AuditIdentity) valid() bool {
	if a.ActorID == "" || !validID(a.ActorID) || !validID(a.ObjectID) {
		return false
	}
	switch a.Action {
	case "source_test", "notification_settings", "notification_test", "retention_settings", "cleanup":
		return true
	default:
		return false
	}
}
