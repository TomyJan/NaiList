package logging

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// A record is written synchronously under one lock. A broken primary sink is
// reported directly to stderr, together with the already-redacted unwritten data.
type reliableWriter struct {
	mu       sync.Mutex
	primary  io.Writer
	fallback io.Writer
	clean    func(string) string
}

func newReliableWriter(w io.Writer, clean func(string) string) *reliableWriter {
	return &reliableWriter{primary: w, fallback: os.Stderr, clean: clean}
}
func (w *reliableWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := 0
	for written < len(data) {
		n, err := w.primary.Write(data[written:])
		if n < 0 || n > len(data)-written {
			n = 0
			err = io.ErrShortWrite
		}
		written += n
		if err == nil && n == 0 {
			err = io.ErrShortWrite
		}
		if err != nil {
			fmt.Fprintf(w.fallback, "time=%s level=ERROR service=nailist event=logging.write_failed msg=%q error=%q\n", time.Now().Format(time.RFC3339Nano), "日志输出失败，以下为未写入内容", w.clean(err.Error()))
			_, _ = w.fallback.Write(data[written:])
			return written, err
		}
	}
	return written, nil
}
