package groups

import "testing"

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
