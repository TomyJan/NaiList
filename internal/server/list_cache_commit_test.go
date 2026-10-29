package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/groups"
)

// Drop an acknowledgement only after PostgreSQL has committed. Keep the actual
// store, transaction and wire protocol so an error cannot stand in for a write.
func lostCommitPool(t *testing.T, db *pgxpool.Pool) (*pgxpool.Pool, *atomic.Bool) {
	t.Helper()
	cfg := db.Config()
	target := net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var armed atomic.Bool
	var workers sync.WaitGroup
	done := make(chan struct{})
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
				backend, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer backend.Close()
				copied := make(chan struct{})
				go func() { _, _ = io.Copy(backend, client); close(copied) }()
				defer func() { client.Close(); backend.Close(); <-copied }()
				pendingUpdate := false
				for {
					header := make([]byte, 5)
					if _, err := io.ReadFull(backend, header); err != nil {
						return
					}
					payload := make([]byte, int(binary.BigEndian.Uint32(header[1:]))-4)
					if _, err := io.ReadFull(backend, payload); err != nil {
						return
					}
					if header[0] == 'C' && string(payload) == "COMMIT\x00" && armed.CompareAndSwap(true, false) {
						return
					}
					if header[0] == 'C' && strings.HasPrefix(string(payload), "UPDATE ") && armed.CompareAndSwap(true, false) {
						pendingUpdate = true
					}
					if pendingUpdate && header[0] == 'Z' && string(payload) == "I" {
						return
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
	cfg.ConnConfig.TLSConfig = nil
	cfg.ConnConfig.Fallbacks = nil
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); listener.Close(); <-done; workers.Wait() })
	return pool, &armed
}

func TestPostgresPublicCacheReloadsAfterLostCommit(t *testing.T) {
	for _, kind := range []string{"groups", "sites"} {
		for _, operation := range []string{"create", "update", "delete", "admin"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				db := securityDatabase(t)
				ctx := context.Background()
				var owner, destination int64
				for _, id := range []*int64{&owner, &destination} {
					if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('commit-cache') RETURNING id`).Scan(id); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken("commit-cache"), owner); err != nil {
					t.Fatal(err)
				}
				proxy, armed := lostCommitPool(t, db)
				s := New(Config{PublicURL: "https://groups.example", AdminUserIDs: map[int64]bool{owner: true}}, proxy, groups.Store{DB: proxy})
				h := s.Routes()
				request := func(method, path, body string) *httptest.ResponseRecorder {
					r := httptest.NewRequest(method, path, strings.NewReader(body))
					r.AddCookie(&http.Cookie{Name: "nailist_session", Value: "commit-cache"})
					r.Header.Set("Origin", "https://groups.example")
					r.Header.Set("X-CSRF-Token", "csrf")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					return w
				}
				body := `{"organization":"before","qqNumber":"123456789","keywords":"","joinUrl":"https://qm.qq.com/q/test"}`
				if kind == "sites" {
					body = `{"organization":"before","name":"test","domain":"commit.example"}`
				}
				path := "/api/" + kind
				var id int64
				if operation != "create" {
					w := request("POST", path, body)
					if w.Code != 201 {
						t.Fatalf("setup: %d %s", w.Code, w.Body.String())
					}
					var saved struct{ ID int64 }
					if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
						t.Fatal(err)
					}
					id = saved.ID
				}
				if w := request("GET", path, ""); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				method, writePath := "POST", path
				body = strings.Replace(body, "before", "committed", 1)
				if operation != "create" {
					method, writePath = "PUT", fmt.Sprintf("%s/%d", path, id)
				}
				if operation == "delete" {
					method = "DELETE"
				}
				if operation == "admin" {
					writePath = fmt.Sprintf("/api/admin/%s/%d", kind, id)
					body = strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"ownerId":%d}`, destination)
				}
				armed.Store(true)
				w := request(method, writePath, body)
				if armed.Load() || w.Code != 500 {
					t.Fatalf("fault was not observed: armed=%v status=%d body=%s", armed.Load(), w.Code, w.Body.String())
				}
				var active int
				query := "SELECT count(*) FROM " + kind + " WHERE deleted_at IS NULL"
				if operation != "delete" {
					query += " AND organization='committed'"
				}
				if err := db.QueryRow(ctx, query).Scan(&active); err != nil {
					t.Fatal(err)
				}
				want := 1
				if operation == "delete" {
					want = 0
				}
				if active != want {
					t.Fatalf("committed rows = %d, want %d", active, want)
				}
				if operation == "admin" {
					var got int64
					if err := db.QueryRow(ctx, "SELECT owner_id FROM "+kind+" WHERE id=$1", id).Scan(&got); err != nil || got != destination {
						t.Fatalf("transfer: owner=%d err=%v", got, err)
					}
				}
				w = request("GET", path, "")
				if w.Code != 200 || (operation == "delete" && strings.TrimSpace(w.Body.String()) != "[]") || (operation != "delete" && !strings.Contains(w.Body.String(), "committed")) {
					t.Fatalf("stale public snapshot after committed write: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
