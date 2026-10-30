package server

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/groups"
)

// Forward COMMIT, lose the application connection, and keep the backend alive
// until PostgreSQL finishes. Cancellation packets follow the broken path too.
func pendingCommitPool(t *testing.T, db *pgxpool.Pool) (*pgxpool.Pool, *atomic.Bool, <-chan struct{}) {
	t.Helper()
	cfg := db.Config()
	target := net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var armed atomic.Bool
	var workers sync.WaitGroup
	done, committed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer client.Close()
				header := make([]byte, 4)
				if _, err := io.ReadFull(client, header); err != nil {
					return
				}
				startup := make([]byte, int(binary.BigEndian.Uint32(header))-4)
				if _, err := io.ReadFull(client, startup); err != nil {
					return
				}
				if binary.BigEndian.Uint32(startup[:4]) == 80877102 {
					return
				}
				backend, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer backend.Close()
				if _, err := backend.Write(append(header, startup...)); err != nil {
					return
				}
				var broken atomic.Bool
				copied := make(chan struct{})
				go func() {
					defer close(copied)
					for {
						header := make([]byte, 5)
						if _, err := io.ReadFull(client, header); err != nil {
							return
						}
						payload := make([]byte, int(binary.BigEndian.Uint32(header[1:]))-4)
						if _, err := io.ReadFull(client, payload); err != nil {
							return
						}
						if header[0] == 'Q' && string(payload) == "commit\x00" && armed.CompareAndSwap(true, false) {
							broken.Store(true)
							if _, err := backend.Write(append(header, payload...)); err != nil {
								return
							}
							client.Close()
							return
						}
						if _, err := backend.Write(append(header, payload...)); err != nil {
							return
						}
					}
				}()
				defer func() { client.Close(); backend.Close(); <-copied }()
				for {
					header := make([]byte, 5)
					if _, err := io.ReadFull(backend, header); err != nil {
						return
					}
					payload := make([]byte, int(binary.BigEndian.Uint32(header[1:]))-4)
					if _, err := io.ReadFull(backend, payload); err != nil {
						return
					}
					if broken.Load() {
						if header[0] == 'Z' {
							close(committed)
							return
						}
						continue
					}
					if _, err := client.Write(append(header, payload...)); err != nil {
						return
					}
				}
			}()
		}
	}()
	cfg.ConnConfig.Host = "127.0.0.1"
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	p, _ := strconv.Atoi(port)
	cfg.ConnConfig.Port = uint16(p)
	cfg.ConnConfig.TLSConfig, cfg.ConnConfig.Fallbacks = nil, nil
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); listener.Close(); <-done; workers.Wait() })
	return pool, &armed, committed
}

func TestPostgresPublicCacheRecoversFromCommitAfterError(t *testing.T) {
	for _, kind := range []string{"groups", "sites"} {
		t.Run(kind, func(t *testing.T) {
			db := securityDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var owner int64
			if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('late commit') RETURNING id`).Scan(&owner); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken("late-commit"), owner); err != nil {
				t.Fatal(err)
			}
			lock := time.Now().UnixNano() & 0x7fffffff
			if _, err := db.Exec(ctx, `CREATE FUNCTION delay_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(`+strconv.FormatInt(lock, 10)+`); RETURN NEW; END $$; CREATE CONSTRAINT TRIGGER delay_commit AFTER INSERT ON `+kind+` DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION delay_commit()`); err != nil {
				t.Fatal(err)
			}
			proxy, armed, committed := pendingCommitPool(t, db)
			gate, err := db.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Release()
			if _, err := gate.Exec(ctx, `SELECT pg_advisory_lock($1)`, lock); err != nil {
				t.Fatal(err)
			}
			defer gate.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lock)
			h := New(Config{PublicURL: "https://groups.example"}, proxy, groups.Store{DB: proxy}).Routes()
			path := "/api/" + kind
			request := func(method, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
				r.AddCookie(&http.Cookie{Name: "nailist_session", Value: "late-commit"})
				r.Header.Set("Origin", "https://groups.example")
				r.Header.Set("X-CSRF-Token", "csrf")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			if w := request("GET", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
				t.Fatal("could not prime empty cache")
			}
			body := `{"organization":"late committed","qqNumber":"123456789","keywords":"","joinUrl":"https://qm.qq.com/q/test"}`
			if kind == "sites" {
				body = `{"organization":"late committed","name":"site","domain":"late.example"}`
			}
			armed.Store(true)
			if w := request("POST", body); w.Code != 500 || armed.Load() {
				t.Fatalf("expected lost connection: %d %s", w.Code, w.Body.String())
			}
			for {
				var waiting int
				if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND objid=$1 AND NOT granted`, lock).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting > 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if w := request("GET", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
				t.Fatal("expected pre-commit cache refill")
			}
			if _, err := gate.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lock); err != nil {
				t.Fatal(err)
			}
			select {
			case <-committed:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var count int
			if err := db.QueryRow(ctx, "SELECT count(*) FROM "+kind+" WHERE organization='late committed'").Scan(&count); err != nil || count != 1 {
				t.Fatalf("commit did not finish: count=%d err=%v", count, err)
			}
			deadline := time.NewTimer(7 * time.Second)
			defer deadline.Stop()
			for {
				w := request("GET", "")
				if w.Code != 200 {
					t.Fatalf("public read: %d %s", w.Code, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "late committed") {
					break
				}
				select {
				case <-deadline.C:
					t.Fatalf("delayed commit permanently hidden by refilled cache: %s", w.Body.String())
				case <-time.After(25 * time.Millisecond):
				}
			}
		})
	}
}
