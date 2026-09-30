package ingest

import (
	"bytes"
	"crypto/sha256"
	"math"
	"sort"
)

const (
	PaginationFailure FailureKind = "pagination"
	TotalChanged      FailureKind = "total_changed"
	DuplicatePage     FailureKind = "duplicate_page"
	DuplicateKey      FailureKind = "duplicate_key"
	AcquisitionLimit  FailureKind = "acquisition_limit"
)

type PageLimits struct {
	MaxPages int
	// MaxBytes == 0 disables only the cumulative byte cap.
	MaxRecords, MaxBytes int64
}
type PageSummary struct {
	Number, Count int
	Bytes         int64
	Digest        [32]byte
}
type Manifest struct {
	Source             string
	Total, Rows, Bytes int64
	Pages              []PageSummary
	Complete           bool
}

// Pager is owned by one logical acquisition, starting at page one. It keeps
// bounded evidence and keys, not the complete source rows. Failure is terminal.
type Pager struct {
	definition      Definition
	pageSize        int
	limits          PageLimits
	manifest        Manifest
	started, failed bool
	keys            map[string]bool
	digests         map[[32]byte]bool
}

func NewPager(source string, pageSize int, limits PageLimits) (*Pager, error) {
	d, ok := definitions[source]
	if !ok || pageSize < 1 || limits.MaxPages < 1 || limits.MaxRecords < 1 || limits.MaxBytes < 0 {
		return nil, &Failure{Kind: ConfigurationFailure}
	}
	return &Pager{definition: d, pageSize: pageSize, limits: limits, manifest: Manifest{Source: source}, keys: map[string]bool{}, digests: map[[32]byte]bool{}}, nil
}

func (p *Pager) Manifest() Manifest {
	result := p.manifest
	result.Pages = append([]PageSummary(nil), result.Pages...)
	return result
}
func (p *Pager) Abort() { p.failed = true; p.manifest.Complete = false }

func (p *Pager) Accept(number int, page Page) error {
	fail := func(kind FailureKind) error { p.Abort(); return &Failure{Kind: kind, Attempt: page.Attempt} }
	if p.failed || p.manifest.Complete || number != len(p.manifest.Pages)+1 || page.Total < 0 {
		return fail(PaginationFailure)
	}
	if p.started && page.Total != p.manifest.Total {
		return fail(TotalChanged)
	}
	pageCount := page.Total / int64(p.pageSize)
	if page.Total%int64(p.pageSize) != 0 {
		pageCount++
	}
	if pageCount == 0 {
		pageCount = 1
	}
	if page.Total > p.limits.MaxRecords || pageCount > int64(p.limits.MaxPages) || number > p.limits.MaxPages {
		return fail(AcquisitionLimit)
	}
	remaining := page.Total - p.manifest.Rows
	want := int64(p.pageSize)
	if remaining < want {
		want = remaining
	}
	if want < 0 || int64(len(page.Records)) != want {
		return fail(PaginationFailure)
	}
	newKeys := map[string]bool{}
	hashes := make([][32]byte, 0, len(page.Records))
	var size int64
	byteLimit := p.limits.MaxBytes
	if byteLimit == 0 {
		byteLimit = math.MaxInt64 // Keep counters representable without a quota.
	}
	for _, record := range page.Records {
		if record.Key == "" {
			return fail(PaginationFailure)
		}
		if !p.definition.History && (p.keys[record.Key] || newKeys[record.Key]) {
			return fail(DuplicateKey)
		}
		newKeys[record.Key] = true
		canonical, err := record.Canonical()
		if err != nil {
			return fail(RecordFailure)
		}
		if int64(len(canonical)) > byteLimit-p.manifest.Bytes-size {
			return fail(AcquisitionLimit)
		}
		size += int64(len(canonical))
		hashes = append(hashes, sha256.Sum256(canonical))
	}
	// A page is a multiset: row reordering must not hide a repeated nonempty
	// page, while distinct historical versions with the same key remain legal.
	sort.Slice(hashes, func(i, j int) bool { return bytes.Compare(hashes[i][:], hashes[j][:]) < 0 })
	hasher := sha256.New()
	for _, hash := range hashes {
		hasher.Write(hash[:])
	}
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	if len(hashes) > 0 && p.digests[digest] {
		return fail(DuplicatePage)
	}
	if !p.started {
		p.manifest.Total = page.Total
		p.started = true
	}
	if !p.definition.History {
		for key := range newKeys {
			p.keys[key] = true
		}
	}
	p.digests[digest] = true
	p.manifest.Rows += int64(len(page.Records))
	p.manifest.Bytes += size
	p.manifest.Pages = append(p.manifest.Pages, PageSummary{Number: number, Count: len(page.Records), Bytes: size, Digest: digest})
	p.manifest.Complete = p.manifest.Rows == p.manifest.Total && int64(number) == pageCount
	return nil
}
