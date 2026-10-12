package candidate

import (
	"bytes"
	"context"
	"encoding/json"
)

// targetSession is private to one synchronous validation invocation. It shares
// decoded manifest metadata, never row-comparison success or ready evidence.
// Every chunk is still read and hashed by its consumer; final Validate rereads
// the manifest and ALL chunks to cover mutations after initial consumption.
type targetSession struct {
	ctx     context.Context
	files   TargetFiles
	doc     targetDocument
	binding []byte
}

func (f TargetFiles) session(ctx context.Context, m Manifest) (*targetSession, context.CancelFunc, error) {
	doc, err := f.document(ctx, m)
	if err != nil {
		return nil, nil, err
	}
	bound, cancel := context.WithCancel(ctx)
	identity, _ := json.Marshal(m)
	return &targetSession{ctx: bound, files: f, doc: doc, binding: identity}, cancel, nil
}
func (s *targetSession) matches(ctx context.Context, m Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	identity, err := json.Marshal(m)
	if err != nil || !bytes.Equal(identity, s.binding) {
		return ErrBuild
	}
	return nil
}
func (s *targetSession) Open(ctx context.Context, m Manifest, v TableVersion) (Rows, error) {
	if err := s.matches(ctx, m); err != nil {
		return nil, err
	}
	for _, table := range s.doc.Tables {
		if table.Version == v {
			return &fileRows{files: s.files, table: v.Table, chunks: table.Chunks, sessionContext: s.ctx}, nil
		}
	}
	return nil, ErrBuild
}
func (s *targetSession) Validate(ctx context.Context, m Manifest) error {
	if err := s.matches(ctx, m); err != nil {
		return err
	}
	return s.files.Validate(ctx, m)
}
