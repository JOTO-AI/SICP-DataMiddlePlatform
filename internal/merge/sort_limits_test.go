package merge

import (
	"bufio"
	"encoding/binary"
	"errors"
	"testing"
)

type lengthHeaderOnly struct{ calls int }

func (r *lengthHeaderOnly) Read(p []byte) (int, error) {
	r.calls++
	if r.calls > 1 {
		return 0, errors.New("body must not be read")
	}
	var header [5]byte
	binary.BigEndian.PutUint32(header[:4], 257)
	return copy(p, header[:]), nil
}

func TestT04_14CorruptRunLengthIsRejectedBeforeReadingOrAllocatingBody(t *testing.T) {
	source := &lengthHeaderOnly{}
	r := runReader{input: bufio.NewReader(source), maxRecordBytes: 256}
	if _, err := r.next(); !errors.Is(err, ErrSpool) {
		t.Fatal("oversized length accepted")
	}
	if source.calls != 1 {
		t.Fatal("attempted body read beyond this stream's byte budget")
	}
}
