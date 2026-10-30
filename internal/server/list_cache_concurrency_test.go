package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"nailist/internal/groups"
)

func TestUncertainListCacheKeepsRefreshingAcrossLaterWrites(t *testing.T) {
	for _, laterWrite := range []error{nil, groups.ErrDuplicate} {
		t.Run(fmt.Sprint(laterWrite), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var cache listCache
				rows := []string{"before"}
				queries := 0
				load := func(context.Context) (listSnapshot, error) { queries++; return encodeList(rows, nil) }
				read := func(want string) {
					t.Helper()
					snapshot, err := cache.get(t.Context(), "groups", load)
					if err != nil || string(snapshot.body) != want {
						t.Fatalf("snapshot=%s err=%v, want %s", snapshot.body, err, want)
					}
				}
				read("[\"before\"]\n")
				cache.afterWrite(t.Context(), "groups", errors.New("commit outcome unknown"))
				read("[\"before\"]\n")
				// A later successful write or business rejection cannot prove that
				// the original transaction has finished, even many intervals later.
				for i := 0; i < 3; i++ {
					time.Sleep(5 * time.Second)
					cache.afterWrite(t.Context(), "groups", laterWrite)
					read("[\"before\"]\n")
				}
				rows = []string{"late commit"}
				read("[\"before\"]\n") // Keep coalescing reads within the interval.
				time.Sleep(5 * time.Second)
				read("[\"late commit\"]\n")
				if queries != 6 {
					t.Fatalf("queries=%d, want 6", queries)
				}
			})
		})
	}
}

func TestCertainListCacheRemainsPermanent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache listCache
		queries := 0
		load := func(context.Context) (listSnapshot, error) { queries++; return encodeList([]int{1}, nil) }
		if _, err := cache.get(t.Context(), "sites", load); err != nil {
			t.Fatal(err)
		}
		time.Sleep(24 * time.Hour)
		cache.afterWrite(t.Context(), "sites", groups.ErrDuplicate)
		if _, err := cache.get(t.Context(), "sites", load); err != nil {
			t.Fatal(err)
		}
		if queries != 1 {
			t.Fatalf("certain cache unexpectedly refreshed: %d queries", queries)
		}
	})
}

func TestUncertainListCacheDoesNotExtendLifetimeWithSlowLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache listCache
		cache.afterWrite(t.Context(), "groups", errors.New("outcome unknown"))
		queries := 0
		load := func(context.Context) (listSnapshot, error) {
			queries++
			if queries == 1 {
				time.Sleep(10 * time.Second)
				return encodeList([]string{"before"}, nil)
			}
			return encodeList([]string{"after"}, nil)
		}
		if _, err := cache.get(t.Context(), "groups", load); err != nil {
			t.Fatal(err)
		}
		snapshot, err := cache.get(t.Context(), "groups", load)
		if err != nil || string(snapshot.body) != "[\"after\"]\n" {
			t.Fatalf("slow old query extended cache lifetime: %s %v", snapshot.body, err)
		}
	})
}

func cacheTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestListCacheCoalescesConcurrentReads(t *testing.T) {
	var cache listCache
	ctx := cacheTestContext(t)
	var queries atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	load := func(ctx context.Context) (listSnapshot, error) {
		if queries.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return encodeList([]int{1, 2, 3}, nil)
		case <-ctx.Done():
			return listSnapshot{}, ctx.Err()
		}
	}
	var launched sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < cap(results); i++ {
		launched.Add(1)
		go func() {
			launched.Done()
			snapshot, err := cache.get(ctx, "groups", load)
			if err == nil && (string(snapshot.body) != "[1,2,3]\n" || snapshot.count != 3) {
				err = errors.New("incorrect concurrent snapshot")
			}
			results <- err
		}()
	}
	launched.Wait()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(release)
	for i := 0; i < cap(results); i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if queries.Load() != 1 {
		t.Fatalf("concurrent reads caused %d queries", queries.Load())
	}
}

func TestListCacheDiscardsQueryInvalidatedDuringLoad(t *testing.T) {
	var cache listCache
	ctx := cacheTestContext(t)
	started, release := make(chan struct{}), make(chan struct{})
	var queries atomic.Int32
	load := func(ctx context.Context) (listSnapshot, error) {
		if queries.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				return encodeList([]string{"old"}, nil)
			case <-ctx.Done():
				return listSnapshot{}, ctx.Err()
			}
		}
		return encodeList([]string{"new"}, nil)
	}
	result := make(chan listSnapshot, 1)
	go func() { snapshot, _ := cache.get(ctx, "groups", load); result <- snapshot }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cache.invalidate(ctx, "groups", false)
	close(release)
	select {
	case snapshot := <-result:
		if string(snapshot.body) != "[\"new\"]\n" {
			t.Fatalf("stale query restored invalidated cache: %s", snapshot.body)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := cache.get(ctx, "groups", load); err != nil {
		t.Fatal(err)
	}
	if queries.Load() != 2 {
		t.Fatalf("expected old query discarded and one reload, got %d", queries.Load())
	}
}

func TestListCacheFailureIsRetried(t *testing.T) {
	var cache listCache
	ctx := cacheTestContext(t)
	queries := 0
	wantErr := errors.New("database unavailable")
	load := func(context.Context) (listSnapshot, error) {
		queries++
		if queries == 1 {
			return listSnapshot{}, wantErr
		}
		return encodeList([]int{}, nil)
	}
	if _, err := cache.get(ctx, "sites", load); !errors.Is(err, wantErr) {
		t.Fatalf("query error lost: %v", err)
	}
	for i := 0; i < 100; i++ {
		snapshot, err := cache.get(ctx, "sites", load)
		if err != nil || string(snapshot.body) != "[]\n" {
			t.Fatalf("retry failed: %s %v", snapshot.body, err)
		}
	}
	if queries != 2 {
		t.Fatalf("empty snapshot was not retained: %d queries", queries)
	}
}

func TestListCacheWaitingCallerCanCancel(t *testing.T) {
	var cache listCache
	ctx := cacheTestContext(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	finished := make(chan error, 1)
	go func() {
		_, err := cache.get(ctx, "groups", func(ctx context.Context) (listSnapshot, error) {
			close(started)
			select {
			case <-release:
				return encodeList([]int{1}, nil)
			case <-ctx.Done():
				return listSnapshot{}, ctx.Err()
			}
		})
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitingCtx, cancel := context.WithCancel(ctx)
	waiting := make(chan error, 1)
	go func() {
		_, err := cache.get(waitingCtx, "groups", func(context.Context) (listSnapshot, error) { return listSnapshot{}, errors.New("duplicate query") })
		waiting <- err
	}()
	cancel()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("cancelled waiter blocked")
	}
	select {
	case err := <-finished:
		t.Fatalf("waiter cancelled leader: %v", err)
	default:
	}
}

func TestListCacheCancelledLoaderReleasesWaiters(t *testing.T) {
	var cache listCache
	ctx := cacheTestContext(t)
	leaderCtx, cancel := context.WithCancel(ctx)
	started := make(chan struct{})
	leader := make(chan error, 1)
	go func() {
		_, err := cache.get(leaderCtx, "sites", func(ctx context.Context) (listSnapshot, error) {
			close(started)
			<-ctx.Done()
			return listSnapshot{}, ctx.Err()
		})
		leader <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result := make(chan error, 1)
	go func() {
		snapshot, err := cache.get(ctx, "sites", func(context.Context) (listSnapshot, error) { return encodeList([]int{2}, nil) })
		if err == nil && string(snapshot.body) != "[2]\n" {
			err = errors.New("cancelled load poisoned snapshot")
		}
		result <- err
	}()
	cancel()
	select {
	case err := <-leader:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("loader cancellation left waiters blocked")
	}
}

func TestListCachePanicReleasesFlight(t *testing.T) {
	var cache listCache
	ctx := cacheTestContext(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing loader panic")
			}
		}()
		_, _ = cache.get(ctx, "groups", func(context.Context) (listSnapshot, error) { panic("broken loader") })
	}()
	snapshot, err := cache.get(ctx, "groups", func(context.Context) (listSnapshot, error) { return encodeList([]int{1}, nil) })
	if err != nil || string(snapshot.body) != "[1]\n" {
		t.Fatalf("panic poisoned cache: %s %v", snapshot.body, err)
	}
}

func BenchmarkPublicListSnapshot(b *testing.B) {
	rows := make([]groups.Group, 1000)
	for i := range rows {
		rows[i] = groups.Group{ID: int64(i + 1), Organization: "兰州理工大学", QQNumber: fmt.Sprint(700000000 + i), JoinURL: "https://qm.qq.com/q/test", Keywords: []string{"兰理", "烂梨"}}
	}
	load := func(context.Context) (listSnapshot, error) { return encodeList(rows, nil) }
	b.Run("encode_each_read", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := load(b.Context()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("memory_snapshot", func(b *testing.B) {
		var cache listCache
		if _, err := cache.get(b.Context(), "groups", load); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := cache.get(b.Context(), "groups", load); err != nil {
				b.Fatal(err)
			}
		}
	})
}
