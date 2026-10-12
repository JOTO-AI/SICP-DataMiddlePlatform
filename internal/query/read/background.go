package read

import "time"

const MaxBackgroundTableRows int64 = 10000000

// BackgroundLimits is for synchronous streaming enumeration, never HTTP input.
// Repeated joins are bounded separately from manifest cardinality. Collect has
// a shared retained-payload cap even though streaming traffic is not capped.
func BackgroundLimits(duration time.Duration) Limits {
	return Limits{Rows: 1000000000, Batches: 10000000, Bytes: 64 << 20, BatchBytes: 16 << 20, BatchSize: 1000, Duration: duration}
}

// TableLimits permits exactly the trusted table count plus an EOF probe. An
// extra row is rejected by Scan before consumption; missing rows by the target
// comparator. Reject implausible declarations before any integer conversion.
func TableLimits(rows int64, duration time.Duration) (Limits, error) {
	if rows < 0 || rows > MaxBackgroundTableRows {
		return Limits{}, ErrBudget
	}
	l := BackgroundLimits(duration)
	l.Rows = int(max(rows, 1))
	l.Batches = int(rows)/l.BatchSize + 2
	return l, nil
}
