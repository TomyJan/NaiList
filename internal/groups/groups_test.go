package groups

import (
	"strings"
	"testing"
)

func TestGroupInputLimits(t *testing.T) {
	valid := Input{Organization: "兰州理工大学", QQNumber: "12345", JoinURL: "https://qm.qq.com/q/AjLVv044nu"}
	for _, test := range []struct {
		name, field, value string
		valid              bool
	}{
		{"five digits", "number", "00000", true},
		{"eleven digits", "number", "12345678901", true},
		{"twelve digits", "number", "123456789012", false},
		{"four digits", "number", "1234", false},
		{"organization boundary", "organization", strings.Repeat("🍐", 32), true},
		{"organization overflow", "organization", strings.Repeat("校", 33), false},
		{"keywords boundary", "keywords", strings.Repeat("🍐", 128), true},
		{"keywords overflow", "keywords", strings.Repeat("梨", 129), false},
		{"short link", "link", valid.JoinURL, true},
		{"trimmed link", "link", " " + valid.JoinURL + "\u00a0", true},
		{"http link", "link", "http://qm.qq.com/q/abc", false},
		{"other domain", "link", "https://example.com/q/abc", false},
		{"host suffix", "link", "https://qm.qq.com.evil/q/abc", false},
		{"credentials", "link", "https://user@qm.qq.com/q/abc", false},
		{"missing code", "link", "https://qm.qq.com/q/", false},
		{"other QQ path", "link", "https://qm.qq.com/abc", false},
		{"path suffix", "link", "https://qm.qq.com/q/abc/extra", false},
		{"embedded whitespace", "link", "https://qm.qq.com/q/ab cd", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			switch test.field {
			case "number":
				input.QQNumber = test.value
			case "organization":
				input.Organization = test.value
			case "keywords":
				input.Keywords = test.value
			case "link":
				input.JoinURL = test.value
			}
			_, err := Normalize(input)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}

func TestKeywordsAndValidation(t *testing.T) {
	g := Input{Organization: " 兰州理工大学 ", Keywords: "烂梨, 兰理，兰理工,烂🍐,兰理", QQNumber: "123456789", JoinURL: "https://qm.qq.com/q/test"}
	v, err := Normalize(g)
	if err != nil {
		t.Fatal(err)
	}
	if v.Organization != "兰州理工大学" || len(v.Keywords) != 4 || v.Keywords[3] != "烂🍐" {
		t.Fatalf("unexpected normalization: %+v", v)
	}
	g.Keywords = ""
	if _, err := Normalize(g); err != nil {
		t.Fatalf("keywords optional: %v", err)
	}
	for _, number := range []string{"", "abc", "123", "123 456", "123456789012345678901"} {
		g.QQNumber = number
		if _, err := Normalize(g); err == nil {
			t.Fatalf("accepted invalid number %q", number)
		}
	}
}

func TestRejectUnsafeLinksAndMissingOrganization(t *testing.T) {
	for _, link := range []string{"javascript:alert(1)", "http://", "file:///etc/passwd", "https://user:password@example.com", "https://example.com/\nfoo"} {
		if _, err := Normalize(Input{Organization: "兰理", QQNumber: "123456789", JoinURL: link}); err == nil {
			t.Fatalf("accepted %q", link)
		}
	}
	if _, err := Normalize(Input{QQNumber: "123456789", JoinURL: "https://qm.qq.com/q/test"}); err == nil {
		t.Fatal("organization required")
	}
}
