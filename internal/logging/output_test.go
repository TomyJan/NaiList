package logging

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

type partialSink struct{ bytes.Buffer }

func (w *partialSink) Write(p []byte) (int, error) {
	if len(p) > 5 {
		p = p[:5]
	}
	return w.Buffer.Write(p)
}

type failedSink struct{}

func (failedSink) Write([]byte) (int, error) { return 0, errors.New("disk-secret unavailable") }

func TestLogOutputHandlesPartialWritesAndReportsSinkFailure(t *testing.T) {
	clean := func(s string) string { return strings.ReplaceAll(s, "disk-secret", "[REDACTED]") }
	var partial partialSink
	writer := newReliableWriter(&partial, clean)
	const record = "time=now level=INFO msg=complete\n"
	if n, err := writer.Write([]byte(record)); err != nil || n != len(record) || partial.String() != record {
		t.Fatalf("partial record lost: %d %v %q", n, err, partial.String())
	}
	var fallback bytes.Buffer
	writer = newReliableWriter(failedSink{}, clean)
	writer.fallback = &fallback
	if _, err := writer.Write([]byte(record)); err == nil {
		t.Fatal("sink failure hidden")
	}
	if !strings.Contains(fallback.String(), "event=logging.write_failed") || !strings.Contains(fallback.String(), record) || strings.Contains(fallback.String(), "disk-secret") {
		t.Fatalf("unsafe or incomplete fallback: %s", fallback.String())
	}
}

func TestConcurrentLogRecordsRemainWhole(t *testing.T) {
	var out bytes.Buffer
	logger, _ := New(&out, "debug")
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.With("request_id", i).Info("并发事件", "event", "test.concurrent", "password", "private-value")
		}(i)
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 64 || strings.Contains(out.String(), "private-value") {
		t.Fatalf("record loss or secret leaked: %s", out.String())
	}
	for _, line := range lines {
		if strings.Count(line, "event=test.concurrent") != 1 || !strings.Contains(line, "request_id=") {
			t.Fatalf("interleaved record: %s", line)
		}
	}
}
