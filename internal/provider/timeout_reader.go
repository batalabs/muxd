package provider

import (
	"io"
	"sync"
	"time"
)

// streamIdleTimeout is the maximum time to wait for new SSE data before
// assuming the API has stalled. If no bytes arrive within this window,
// the underlying body is closed, causing the scanner to return an error.
const streamIdleTimeout = 3 * time.Minute

// timeoutReader wraps an io.ReadCloser and closes it if a Read blocks
// for longer than the configured idle timeout. This prevents SSE stream
// parsers from hanging indefinitely when the API stalls mid-stream.
type timeoutReader struct {
	rc      io.ReadCloser
	timer   *time.Timer
	once    sync.Once
}

func newTimeoutReader(rc io.ReadCloser) *timeoutReader {
	tr := &timeoutReader{rc: rc}
	tr.timer = time.AfterFunc(streamIdleTimeout, func() {
		tr.once.Do(func() { rc.Close() })
	})
	return tr
}

func (r *timeoutReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		r.timer.Reset(streamIdleTimeout)
	}
	return n, err
}

func (r *timeoutReader) Close() error {
	r.timer.Stop()
	var err error
	r.once.Do(func() { err = r.rc.Close() })
	return err
}
