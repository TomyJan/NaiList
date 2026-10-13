package server

import (
	"io/fs"
	"nailist/web"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOversizedRangeDoesNotProduceThousandsOfParts(t *testing.T) {
	files, err := fs.ReadDir(web.Files(), "assets")
	if err != nil {
		t.Fatal(err)
	}
	var asset string
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".wasm") {
			asset = "/assets/" + file.Name()
			break
		}
	}
	if asset == "" {
		t.Fatal("build frontend before running tests")
	}
	handler := New(Config{}, nil, &memoryGroups{}).Routes()
	for _, count := range []int{1, 5000} {
		r := httptest.NewRequest("GET", asset, nil)
		r.Header.Set("Range", "bytes="+strings.TrimSuffix(strings.Repeat("0-0,", count), ","))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if count == 1 && w.Code != 206 {
			t.Fatalf("ordinary range rejected: %d", w.Code)
		}
		if count > 1 && w.Code == 206 {
			t.Fatal("excessive ranges still processed as multipart; upgrade Go toolchain")
		}
	}
}
