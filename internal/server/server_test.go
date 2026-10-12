package server

import (
	"context"
	"encoding/json"
	"nailist/internal/groups"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memoryGroups struct{ items []groups.Group }

func (m *memoryGroups) List(_ context.Context, owner int64) ([]groups.Group, error) {
	out := []groups.Group{}
	for _, g := range m.items {
		if owner == 0 || g.OwnerID == owner {
			out = append(out, g)
		}
	}
	return out, nil
}
func (m *memoryGroups) Save(_ context.Context, owner, id int64, g groups.Group) (groups.Group, error) {
	if id > 0 {
		for i, v := range m.items {
			if v.ID == id && v.OwnerID == owner {
				g.ID = id
				g.OwnerID = owner
				m.items[i] = g
				return g, nil
			}
		}
		return g, groups.ErrNotFound
	}
	g.ID = int64(len(m.items) + 1)
	g.OwnerID = owner
	m.items = append(m.items, g)
	return g, nil
}
func (m *memoryGroups) Delete(_ context.Context, owner, id int64) error {
	for i, g := range m.items {
		if g.ID == id && g.OwnerID == owner {
			m.items = append(m.items[:i], m.items[i+1:]...)
			return nil
		}
	}
	return groups.ErrNotFound
}

func TestPublicListIncludesSearchKeywords(t *testing.T) {
	s := New(Config{PublicURL: "http://localhost:8080"}, nil, &memoryGroups{items: []groups.Group{{ID: 1, Organization: "兰州理工大学", Keywords: []string{"烂梨", "烂🍐"}, QQNumber: "123456789"}}})
	r := httptest.NewRecorder()
	s.Routes().ServeHTTP(r, httptest.NewRequest("GET", "/api/groups", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "烂🍐") {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	var out []groups.Group
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil || len(out) != 1 {
		t.Fatal("full list missing")
	}
}
func TestAnonymousWritesRejected(t *testing.T) {
	s := New(Config{PublicURL: "http://localhost:8080"}, nil, &memoryGroups{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		path := "/api/groups"
		if method != "POST" {
			path += "/1"
		}
		r := httptest.NewRecorder()
		s.Routes().ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if r.Code != 401 {
			t.Fatalf("%s: %d", method, r.Code)
		}
	}
}
func TestOwnerIsolation(t *testing.T) {
	store := &memoryGroups{items: []groups.Group{{ID: 1, OwnerID: 9}}}
	s := New(Config{PublicURL: "http://localhost:8080"}, nil, store)
	r := httptest.NewRequest("PUT", "/api/groups/1", strings.NewReader(`{"organization":"兰理","qqNumber":"123456789","joinUrl":"https://qm.qq.com/q/test"}`))
	r = r.WithContext(context.WithValue(r.Context(), userKey, User{ID: 8, CSRF: "test"}))
	r.Header.Set("X-CSRF-Token", "test")
	out := httptest.NewRecorder()
	s.Routes().ServeHTTP(out, r)
	if out.Code != 404 {
		t.Fatalf("non-owner: %d %s", out.Code, out.Body.String())
	}
}
func TestCrossSiteAndBadCSRFRejected(t *testing.T) {
	s := New(Config{PublicURL: "http://localhost:8080"}, nil, &memoryGroups{})
	for _, origin := range []string{"http://evil.example", ""} {
		r := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{}`))
		r = r.WithContext(context.WithValue(r.Context(), userKey, User{ID: 8, CSRF: "right"}))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", "wrong")
		out := httptest.NewRecorder()
		s.Routes().ServeHTTP(out, r)
		if out.Code != 403 {
			t.Fatalf("bad csrf: %d", out.Code)
		}
	}
}

func TestGroupWritesRejectInvalidFields(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		for _, input := range []groups.Input{
			{Organization: strings.Repeat("校", 33), QQNumber: "12345", JoinURL: "https://qm.qq.com/q/test"},
			{Organization: "学校", Keywords: strings.Repeat("梨", 129), QQNumber: "12345", JoinURL: "https://qm.qq.com/q/test"},
			{Organization: "学校", QQNumber: "123456789012", JoinURL: "https://qm.qq.com/q/test"},
			{Organization: "学校", QQNumber: "12345", JoinURL: "https://example.com/q/test"},
		} {
			store := &memoryGroups{}
			handler := New(Config{PublicURL: "http://localhost:8080"}, nil, store).Routes()
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/groups"
			if method == "PUT" {
				path += "/1"
			}
			r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
			r = r.WithContext(context.WithValue(r.Context(), userKey, User{ID: 1, CSRF: "csrf"}))
			r.Header.Set("X-CSRF-Token", "csrf")
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, r)
			if out.Code != 400 || len(store.items) != 0 {
				t.Fatalf("%s: invalid input reached storage: %d %s", method, out.Code, out.Body.String())
			}
		}
	}
}
