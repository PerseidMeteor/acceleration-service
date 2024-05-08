package lion

import "github.com/containerd/containerd/content"

// ReaderAtWrapper is a wrapper around content.ReaderAt to implement io.Reader.
type ReaderAtWrapper struct {
	reader content.ReaderAt
	offset int64
}

func (r *ReaderAtWrapper) Read(p []byte) (n int, err error) {
	n, err = r.reader.ReadAt(p, r.offset)
	r.offset += int64(n)
	return
}

func (r *ReaderAtWrapper) Close() error {
	// Since content.ReaderAt does not need to be closed, this is a no-op.
	return nil
}