package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type NotificationSettings struct {
	Revision   int64           `json:"revision,string"`
	Enabled    bool            `json:"enabled"`
	Recipients []string        `json:"recipients"`
	Rules      map[string]bool `json:"rules"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func NormalizeAddress(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") {
		return "", ErrArgument
	}
	v = strings.TrimSpace(v)
	if len(v) == 0 || len(v) > 254 {
		return "", ErrArgument
	}
	a, e := mail.ParseAddress(v)
	if e != nil || a.Address != v || a.Name != "" {
		return "", ErrArgument
	}
	local, domain, ok := strings.Cut(v, "@")
	if !ok || local == "" || domain == "" {
		return "", ErrArgument
	}
	return local + "@" + strings.ToLower(domain), nil
}
func (s *NotificationSettings) Normalize(channel bool) error {
	if s.Revision < 1 || len(s.Recipients) > 20 || len(s.Rules) > 3 {
		return ErrArgument
	}
	set := map[string]bool{}
	out := []string{}
	for _, v := range s.Recipients {
		v, e := NormalizeAddress(v)
		if e != nil {
			return e
		}
		if !set[v] {
			set[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	s.Recipients = out
	for key := range s.Rules {
		if key != "task_failed" && key != "source_schema" && key != "source_test_failed" {
			return ErrArgument
		}
	}
	if s.Rules == nil {
		s.Rules = map[string]bool{}
	}
	if len(out) == 0 {
		s.Enabled = false
	}
	if s.Enabled && !channel {
		return ErrUnavailable
	}
	return nil
}
func settings(ctx context.Context, tx storage.Transaction, lock bool) (NotificationSettings, error) {
	q := "SELECT revision,enabled,recipients::text,rules::text,updated_at FROM control.notification_settings WHERE singleton"
	if lock {
		q += " FOR UPDATE"
	}
	var s NotificationSettings
	var recipients, rules string
	if tx.QueryRow(ctx, q).Scan(&s.Revision, &s.Enabled, &recipients, &rules, &s.UpdatedAt) != nil || len(recipients) > 8192 || len(rules) > 2048 || json.Unmarshal([]byte(recipients), &s.Recipients) != nil || json.Unmarshal([]byte(rules), &s.Rules) != nil {
		return s, ErrUnavailable
	}
	if s.Normalize(true) != nil {
		return s, ErrUnavailable
	}
	return s, nil
}
func (r Repository) EnsureNotifications(ctx context.Context, legacy string, channel bool, now time.Time) error {
	s := NotificationSettings{Revision: 1, Recipients: []string{}, Rules: map[string]bool{"source_schema": true, "task_failed": false, "source_test_failed": false}}
	legacyRecipient, legacyErr := NormalizeAddress(legacy)
	if legacyErr == nil {
		if channel {
			s.Enabled = true
			s.Recipients = []string{legacyRecipient}
		}
	}
	recipients, _ := json.Marshal(s.Recipients)
	rules, _ := json.Marshal(s.Rules)
	return r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		count, e := tx.Exec(ctx, "INSERT INTO control.notification_settings(singleton,revision,enabled,recipients,rules,updated_at) VALUES(true,1,$1,$2::jsonb,$3::jsonb,$4) ON CONFLICT(singleton) DO NOTHING", s.Enabled, string(recipients), string(rules), now)
		if e != nil {
			return ErrUnavailable
		}
		if count == 0 {
			return nil
		}
		// An old attempt may already have sent mail. Never reset its budget or
		// silently requeue it as a new error fact, even if settings were disabled.
		if _, e = tx.Exec(ctx, `INSERT INTO control.notification_facts(id,code,task_id,occurred_at,decision,revision) SELECT 'schema:'||id,'source_schema',task_id,created_at,'legacy',1 FROM control.notification_outbox WHERE attempts>0 OR status<>'pending' ON CONFLICT(id) DO NOTHING`); e != nil {
			return ErrUnavailable
		}
		// Historical outcomes belong to the old address even if the transport
		// is currently disabled. Importing history must not enable sending.
		if legacyErr == nil {
			if _, e = tx.Exec(ctx, `INSERT INTO control.notification_deliveries(id,fact_id,rule_id,revision,recipient,status,attempts,created_at,deadline,next_attempt_at,accepted_at)
 SELECT 'legacy:'||id,'schema:'||id,'source_schema',1,$1,CASE WHEN status IN ('accepted','delivered') THEN 'accepted' WHEN payload #>> '{delivery,attempts,-1,result}'='rejected' THEN 'rejected' WHEN attempts>=3 THEN 'exhausted' ELSE 'unknown' END,attempts,created_at,created_at+interval '24 hours',NULL,sent_at FROM control.notification_outbox WHERE attempts>0 OR status<>'pending' ON CONFLICT(id) DO NOTHING`, legacyRecipient); e != nil {
				return ErrUnavailable
			}
			if _, e = tx.Exec(ctx, `INSERT INTO control.notification_attempts(delivery_id,number,started_at,finished_at,outcome) SELECT 'legacy:'||n.id,(a->>'number')::integer,(a->>'started_at')::timestamptz,(a->>'finished_at')::timestamptz,CASE WHEN a->>'result'='delivered' THEN 'accepted' ELSE a->>'result' END FROM control.notification_outbox n CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(n.payload #> '{delivery,attempts}')='array' THEN n.payload #> '{delivery,attempts}' ELSE '[]'::jsonb END) a WHERE n.attempts>0 ON CONFLICT DO NOTHING`); e != nil {
				return ErrUnavailable
			}
		}
		return nil
	})
}
func (r Repository) NotificationSettings(ctx context.Context) (NotificationSettings, error) {
	var s NotificationSettings
	err := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		var e error
		s, e = settings(ctx, tx, false)
		return e
	})
	return s, err
}
func (r Repository) SaveNotifications(ctx context.Context, next NotificationSettings, channel bool, subject string, now time.Time) (NotificationSettings, error) {
	if e := next.Normalize(channel); e != nil {
		return NotificationSettings{}, e
	}
	recipients, _ := json.Marshal(next.Recipients)
	rules, _ := json.Marshal(next.Rules)
	err := r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		old, e := settings(ctx, tx, true)
		if e != nil {
			return e
		}
		if next.Revision != old.Revision {
			return ErrConflict
		}
		next.Revision++
		next.UpdatedAt = now
		if n, e := tx.Exec(ctx, "UPDATE control.notification_settings SET revision=$1,enabled=$2,recipients=$3::jsonb,rules=$4::jsonb,updated_at=$5 WHERE singleton", next.Revision, next.Enabled, string(recipients), string(rules), now); e != nil || n != 1 {
			return ErrUnavailable
		}
		// Same row lock as claim: already-sending mail cannot be recalled.
		if _, e := tx.Exec(ctx, `UPDATE control.notification_deliveries SET status='cancelled' WHERE status IN ('queued','retry') AND (NOT $1 OR NOT ($2::jsonb ? recipient) OR (rule_id<>'test' AND NOT coalesce(($3::jsonb ->> rule_id)::boolean,false)))`, next.Enabled, string(recipients), string(rules)); e != nil {
			return ErrUnavailable
		}
		if _, e := tx.Exec(ctx, "INSERT INTO control.management_audit(id,subject,action,occurred_at) VALUES($1,$2,'notification_settings',$3)", NewID(), subject, now); e != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return NotificationSettings{}, err
	}
	return next, nil
}
func deliveryID(fact, rule, recipient string) string {
	sum := sha256.Sum256([]byte(fact + "\x00" + rule + "\x00" + recipient))
	return hex.EncodeToString(sum[:])
}

// fanout is once per fact; later recipient/rule changes do not expand old facts.
func fanout(ctx context.Context, tx storage.Transaction, s NotificationSettings, id, code string, at, queuedAt time.Time) error {
	decision := "disabled"
	enabled := s.Enabled && (s.Rules[code] || code == "test")
	if enabled {
		decision = "queued"
	}
	n, e := tx.Exec(ctx, "INSERT INTO control.notification_facts(id,code,occurred_at,decision,revision) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING", id, code, at, decision, s.Revision)
	if e != nil {
		return ErrUnavailable
	}
	if n == 0 {
		return nil
	}
	if !enabled {
		return nil
	}
	for _, recipient := range s.Recipients {
		if _, e = tx.Exec(ctx, `INSERT INTO control.notification_deliveries(id,fact_id,rule_id,revision,recipient,status,created_at,deadline) VALUES($1,$2,$3,$4,$5,'queued',$6,$7)`, deliveryID(id, code, recipient), id, code, s.Revision, recipient, queuedAt, queuedAt.Add(24*time.Hour)); e != nil {
			return ErrUnavailable
		}
	}
	return nil
}
func (r Repository) NotificationFacts(ctx context.Context, now time.Time) error {
	return r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		s, e := settings(ctx, tx, true)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE control.notification_attempts a SET outcome='unknown',finished_at=$1 FROM control.notification_deliveries d WHERE a.delivery_id=d.id AND a.number=d.attempts AND d.status='sending' AND d.lease_until<=$1", now); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "UPDATE control.notification_deliveries SET status='unknown' WHERE status='sending' AND lease_until<=$1", now); e != nil {
			return ErrUnavailable
		}
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, `SELECT id,code,at FROM (
 SELECT 'task:'||id AS id,'task_failed' AS code,finished_at AS at FROM control.tasks WHERE status='failed' AND finished_at IS NOT NULL
 UNION ALL SELECT 'schema:'||id,'source_schema',created_at FROM control.notification_outbox
 UNION ALL SELECT 'test:'||id,'source_test_failed',finished_at FROM control.management_jobs WHERE action='source_test' AND status='failed' AND finished_at IS NOT NULL
 ) facts WHERE NOT EXISTS(SELECT 1 FROM control.notification_facts f WHERE f.id=facts.id) ORDER BY at,id LIMIT 100`)
		if e != nil {
			return ErrUnavailable
		}
		var facts []struct {
			id, code string
			at       time.Time
		}
		for rows.Next() {
			var f struct {
				id, code string
				at       time.Time
			}
			if rows.Scan(&f.id, &f.code, &f.at) != nil {
				rows.Close()
				return ErrUnavailable
			}
			facts = append(facts, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return ErrUnavailable
		}
		for _, f := range facts {
			if e = fanout(ctx, tx, s, f.id, f.code, f.at, now); e != nil {
				return e
			}
		}
		return nil
	})
}

type Delivery struct {
	ID            string     `json:"id"`
	FactID        string     `json:"fact_id"`
	Rule          string     `json:"rule_id"`
	Revision      int64      `json:"revision,string"`
	Recipient     string     `json:"recipient"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	CreatedAt     time.Time  `json:"created_at"`
	Deadline      time.Time  `json:"deadline"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	Owner         *string    `json:"-"`
	LeaseUntil    *time.Time `json:"lease_until"`
	AcceptedAt    *time.Time `json:"accepted_at"`
}

const deliveryColumns = "id,fact_id,rule_id,revision,recipient,status,attempts,created_at,deadline,next_attempt_at,owner,lease_until,accepted_at"

func scanDelivery(row storage.Row) (Delivery, error) {
	var d Delivery
	e := row.Scan(&d.ID, &d.FactID, &d.Rule, &d.Revision, &d.Recipient, &d.Status, &d.Attempts, &d.CreatedAt, &d.Deadline, &d.NextAttemptAt, &d.Owner, &d.LeaseUntil, &d.AcceptedAt)
	if e != nil {
		return d, ErrUnavailable
	}
	return d, nil
}
func (r Repository) ClaimDelivery(ctx context.Context, owner string, now time.Time) (*Delivery, error) {
	var out *Delivery
	err := r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		s, e := settings(ctx, tx, true)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE control.notification_attempts a SET outcome='unknown',finished_at=$1 FROM control.notification_deliveries d WHERE a.delivery_id=d.id AND a.number=d.attempts AND d.status='sending' AND d.lease_until<=$1", now); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "UPDATE control.notification_deliveries SET status='unknown' WHERE status='sending' AND lease_until<=$1", now); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "UPDATE control.notification_deliveries SET status='exhausted' WHERE status IN ('queued','retry') AND (deadline<=$1 OR attempts>=3)", now); e != nil {
			return ErrUnavailable
		}
		if !s.Enabled {
			return nil
		}
		var id *string
		if tx.QueryRow(ctx, "SELECT (SELECT id FROM control.notification_deliveries WHERE status IN ('queued','retry') AND deadline>$1 AND attempts<3 AND (next_attempt_at IS NULL OR next_attempt_at<=$1) ORDER BY created_at,id LIMIT 1)", now).Scan(&id) != nil {
			return ErrUnavailable
		}
		if id == nil {
			return nil
		}
		d, e := scanDelivery(tx.QueryRow(ctx, "SELECT "+deliveryColumns+" FROM control.notification_deliveries WHERE id=$1", *id))
		if e != nil {
			return e
		}
		d.Attempts++
		lease := now.Add(time.Minute)
		if _, e = tx.Exec(ctx, "UPDATE control.notification_deliveries SET status='sending',attempts=$2,owner=$3,lease_until=$4 WHERE id=$1", d.ID, d.Attempts, owner, lease); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "INSERT INTO control.notification_attempts(delivery_id,number,started_at,outcome) VALUES($1,$2,$3,'sending')", d.ID, d.Attempts, now); e != nil {
			return ErrUnavailable
		}
		d.Owner = &owner
		d.LeaseUntil = &lease
		d.Status = "sending"
		out = &d
		return nil
	})
	return out, err
}
func (r Repository) FinishDelivery(ctx context.Context, d Delivery, receipt notification.Receipt, sendErr error, now time.Time) error {
	status := "unknown"
	var next *time.Time
	var accepted *time.Time
	if receipt.Accepted && !receipt.Rejected && !receipt.Temporary && sendErr == nil {
		status = "accepted"
		accepted = &now
	} else if receipt.Rejected && !receipt.Accepted && !receipt.Temporary {
		status = "rejected"
	} else if receipt.Temporary && !receipt.Accepted && !receipt.Rejected {
		status = "exhausted"
		delay := time.Minute
		if d.Attempts == 2 {
			delay = 5 * time.Minute
		}
		at := now.Add(delay)
		if d.Attempts < 3 && at.Before(d.Deadline) {
			status = "retry"
			next = &at
		}
	}
	return r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		s, e := settings(ctx, tx, true)
		if e != nil {
			return e
		}
		if status == "retry" {
			allowed := s.Enabled && (s.Rules[d.Rule] || d.Rule == "test")
			found := false
			for _, a := range s.Recipients {
				found = found || a == d.Recipient
			}
			if !allowed || !found {
				status = "cancelled"
				next = nil
			}
		}
		n, e := tx.Exec(ctx, "UPDATE control.notification_deliveries SET status=$4,next_attempt_at=$5,accepted_at=$6 WHERE id=$1 AND owner=$2 AND attempts=$3 AND status='sending' AND lease_until>$7", d.ID, d.Owner, d.Attempts, status, next, accepted, now)
		if e != nil {
			return ErrUnavailable
		}
		if n != 1 {
			return ErrConflict
		}
		if _, e = tx.Exec(ctx, "UPDATE control.notification_attempts SET outcome=$3,finished_at=$4 WHERE delivery_id=$1 AND number=$2", d.ID, d.Attempts, status, now); e != nil {
			return ErrUnavailable
		}
		return nil
	})
}

type Notifier struct {
	Repository            Repository
	Channel               notification.Channel
	Audit                 events.Sink
	From, LegacyTo, Owner string
	channelMu             sync.Mutex
	lastChannelResult     string
	lastChannelAt         time.Time
}

type ChannelDiagnostics struct {
	Configured bool       `json:"configured"`
	LastResult string     `json:"last_result"`
	ObservedAt *time.Time `json:"observed_at"`
}

// This is a local observation of an actual send, not a connectivity probe or
// an assertion that SMTP acceptance delivered the message to an inbox.
func (n *Notifier) ChannelDiagnostics() ChannelDiagnostics {
	n.channelMu.Lock()
	defer n.channelMu.Unlock()
	out := ChannelDiagnostics{Configured: n.Channel != nil, LastResult: "not_observed"}
	if !n.lastChannelAt.IsZero() {
		at := n.lastChannelAt
		out.ObservedAt = &at
		out.LastResult = n.lastChannelResult
	}
	return out
}
func (n *Notifier) observeChannel(r notification.Receipt, at time.Time) {
	result := "unknown"
	switch {
	case r.Accepted:
		result = "accepted"
	case r.Rejected:
		result = "rejected"
	case r.Temporary:
		result = "temporary_failure"
	}
	n.channelMu.Lock()
	n.lastChannelResult = result
	n.lastChannelAt = at
	n.channelMu.Unlock()
}

func (n *Notifier) Ensure(ctx context.Context) error {
	return n.Repository.EnsureNotifications(ctx, n.LegacyTo, n.Channel != nil, time.Now().UTC())
}
func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = n.Pump(ctx)
		}
	}
}
func (n *Notifier) Pump(ctx context.Context) error {
	if e := n.Ensure(ctx); e != nil {
		return e
	}
	if e := n.Repository.NotificationFacts(ctx, time.Now().UTC()); e != nil {
		return e
	}
	_ = n.audit(ctx) // Durable pending facts remain; log failures cannot block SMTP.
	if e := n.test(ctx); e != nil {
		return e
	}
	if n.Channel == nil {
		return nil
	}
	d, e := n.Repository.ClaimDelivery(ctx, n.Owner, time.Now().UTC())
	if e != nil || d == nil {
		return e
	}
	send, cancel := context.WithTimeout(ctx, 30*time.Second)
	receipt, sendErr := n.Channel.Send(send, notification.Mail{ID: d.ID, From: n.From, To: d.Recipient, Subject: "SICP " + d.Rule, Body: "Event reference: " + d.FactID + "\nRule: " + d.Rule})
	cancel()
	n.observeChannel(receipt, time.Now().UTC())
	finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	return n.Repository.FinishDelivery(finish, *d, receipt, sendErr, time.Now().UTC())
}

func (n *Notifier) test(ctx context.Context) error {
	j, e := n.Repository.Claim(ctx, "notification_test", n.Owner, time.Now().UTC())
	if e != nil || j == nil {
		return e
	}
	// The accepted job freezes exactly the saved authorized revision/recipients.
	var saved NotificationSettings
	if json.Unmarshal(j.Request, &saved) != nil {
		return ErrUnavailable
	}
	e = n.Repository.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		// Serialize with claim/recovery before locking settings, the same order
		// as acceptance. An expired executor must never create new deliveries.
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734219100)"); e != nil {
			return ErrUnavailable
		}
		var owns bool
		if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.management_jobs WHERE id=$1 AND owner=$2 AND status='running' AND lease_until>clock_timestamp() AND deadline>clock_timestamp())", j.ID, n.Owner).Scan(&owns) != nil {
			return ErrUnavailable
		}
		if !owns {
			return ErrConflict
		}
		current, e := settings(ctx, tx, true)
		if e != nil {
			return e
		}
		if !current.Enabled || current.Revision != saved.Revision {
			return ErrConflict
		}
		return fanout(ctx, tx, current, "explicit:"+j.ID, "test", j.AcceptedAt, time.Now().UTC())
	})
	status := "succeeded"
	decision := "queued"
	if e != nil {
		status = "failed"
		decision = "settings_changed"
	}
	return n.Repository.Finish(ctx, *j, status, map[string]any{"decision": decision}, time.Now().UTC())
}
func (n *Notifier) audit(ctx context.Context) error {
	if n.Audit == nil {
		return nil
	}
	var rowsToSend []struct {
		id, subject, action string
		at                  time.Time
	}
	e := n.Repository.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, "SELECT id,subject,action,occurred_at FROM control.management_audit WHERE NOT confirmed ORDER BY occurred_at,id LIMIT 20")
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var v struct {
				id, subject, action string
				at                  time.Time
			}
			if rows.Scan(&v.id, &v.subject, &v.action, &v.at) != nil {
				return ErrUnavailable
			}
			rowsToSend = append(rowsToSend, v)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, v := range rowsToSend {
		actor := sha256.Sum256([]byte(v.subject))
		id := sha256.Sum256([]byte(v.id))
		object := hex.EncodeToString(id[:])
		event := events.Event{ID: object + "_audit", EntityID: object, Kind: events.Audit, Phase: events.Finished, Outcome: events.Succeeded, OccurredAt: v.at, Audit: &events.AuditIdentity{ActorID: hex.EncodeToString(actor[:]), Action: v.action, ObjectID: object}}
		if e = n.Audit.Record(ctx, event); e != nil {
			return ErrUnavailable
		}
		if e = n.Repository.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
			_, e := tx.Exec(ctx, "UPDATE control.management_audit SET confirmed=true WHERE id=$1", v.id)
			return e
		}); e != nil {
			return e
		}
	}
	return nil
}
func (r Repository) AcceptNotificationTest(ctx context.Context, subject, key string, revision int64, channel bool, now time.Time) (Job, error) {
	// Only the revision is the canonical idempotency input. Recipients are
	// resolved again inside the shared configuration/admission transaction.
	return r.Accept(ctx, subject, "notification_test", key, struct {
		Revision int64 `json:"revision,string"`
	}{revision}, now, 30*time.Second, func(ctx context.Context, tx storage.Transaction) error {
		s, e := settings(ctx, tx, true)
		if e != nil {
			return e
		}
		if s.Revision != revision {
			return ErrConflict
		}
		if !s.Enabled || !channel {
			return ErrUnavailable
		}
		return nil
	})
}
func (r Repository) Deliveries(ctx context.Context, after string, limit int) ([]Delivery, error) {
	if limit < 1 || limit > 201 {
		return nil, ErrArgument
	}
	out := []Delivery{}
	e := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, "SELECT "+deliveryColumns+" FROM control.notification_deliveries WHERE id>$1 ORDER BY id LIMIT $2", after, limit)
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			d, e := scanDelivery(rows)
			if e != nil || len(out) >= limit {
				return ErrUnavailable
			}
			out = append(out, d)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

type DeliveryAttempt struct {
	Number     int        `json:"number"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Outcome    string     `json:"outcome"`
}

func (r Repository) Delivery(ctx context.Context, id string) (Delivery, []DeliveryAttempt, error) {
	if !ValidID(id) {
		return Delivery{}, nil, ErrArgument
	}
	var d Delivery
	attempts := []DeliveryAttempt{}
	e := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		var exists bool
		if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.notification_deliveries WHERE id=$1)", id).Scan(&exists) != nil {
			return ErrUnavailable
		}
		if !exists {
			return ErrMissing
		}
		var e error
		d, e = scanDelivery(tx.QueryRow(ctx, "SELECT "+deliveryColumns+" FROM control.notification_deliveries WHERE id=$1", id))
		if e != nil {
			return e
		}
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, "SELECT number,started_at,finished_at,outcome FROM control.notification_attempts WHERE delivery_id=$1 ORDER BY number LIMIT 16", id)
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var a DeliveryAttempt
			if rows.Scan(&a.Number, &a.StartedAt, &a.FinishedAt, &a.Outcome) != nil {
				return ErrUnavailable
			}
			attempts = append(attempts, a)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	return d, attempts, e
}
