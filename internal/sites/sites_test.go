package sites

import (
	"strings"
	"testing"
)

func TestNormalizeDomainAndFields(t *testing.T) {
	for _, domain := range []string{" Example.COM ", "WWW.Example.COM", "sub.example.co.uk", "xn--fiqs8s.example"} {
		got, err := Normalize(Input{Name: " 名称 ", Domain: domain, Organization: " 单位 "})
		if err != nil || got.Name != "名称" || got.Organization != "单位" {
			t.Fatalf("%q: %+v %v", domain, got, err)
		}
		if strings.Contains(got.Domain, "www.") || got.Domain != strings.ToLower(strings.TrimSpace(got.Domain)) {
			t.Fatalf("not canonical: %q", got.Domain)
		}
	}
	for _, domain := range []string{"", "example", "https://example.com", "http://example.com", "//example.com", "example.com/a", "example.com?x=1", "example.com#x", "user@example.com", "example.com:443", "127.0.0.1", "[::1]", "example.c", "example.123", "example.", ".example.com", "a..com", "-a.com", "a-.com", "a_b.com", "例子.com:443", "user@例子.com", "例子%2ecom", `例子.com\evil`, "例 子.com", "a b.com", strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 126) + "com"} {
		if _, err := Normalize(Input{Name: "名称", Domain: domain, Organization: "单位"}); err == nil {
			t.Errorf("accepted invalid domain %q", domain)
		}
	}
	for _, in := range []Input{{Name: "", Domain: "example.com", Organization: "单位"}, {Name: strings.Repeat("奶", 33), Domain: "example.com", Organization: "单位"}, {Name: "名称", Domain: "example.com", Organization: ""}, {Name: "名称", Domain: "example.com", Organization: strings.Repeat("奶", 33)}} {
		if _, err := Normalize(in); err == nil {
			t.Errorf("accepted invalid fields: %+v", in)
		}
	}
	if _, err := Normalize(Input{Name: strings.Repeat("奶", 32), Domain: "example.com", Organization: strings.Repeat("奶", 32)}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeChineseDomain(t *testing.T) {
	for _, domain := range []string{"甘肃奶龙大学.com", " WWW.甘肃奶龙大学.COM ", "xn--psszg58cy52cu6lj57b.com"} {
		got, err := Normalize(Input{Name: "甘肃奶龙大学", Domain: domain, Organization: "兰州理工大学"})
		if err != nil || got.Domain != "xn--psszg58cy52cu6lj57b.com" {
			t.Fatalf("%q: %+v %v", domain, got, err)
		}
	}
}
