package managed

import "testing"

const (
	testOwner = "nixos"
	testBare  = "pool"
)

func TestParseSelector(t *testing.T) {
	cases := []struct {
		in      string
		want    Selector
		wantErr bool
	}{
		{in: "a.b/c=true", want: Selector{Key: "a.b/c", Value: "true", HasValue: true}},
		{in: " key ", want: Selector{Key: "key"}},
		{in: "k=", want: Selector{Key: "k", HasValue: true}},
		{in: "k=a=b", want: Selector{Key: "k", Value: "a=b", HasValue: true}},
		{in: "=v", wantErr: true},
		{in: "  ", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseSelector(c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("%q: err=%v wantErr=%v", c.in, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Errorf("%q: got %+v want %+v", c.in, got, c.want)
		}
	}
}

func TestPolicyCheck(t *testing.T) {
	p := NewPolicy([]string{"ci.managed=true", testBare, ""})
	cases := []struct {
		name   string
		labels map[string]string
		by     string
		ok     bool
	}{
		{"none", map[string]string{"x": "y"}, "", false},
		{"nil", nil, "", false},
		{"dmanager label", map[string]string{Label: testOwner}, testOwner, true},
		{"empty dmanager label", map[string]string{Label: ""}, "", false},
		{"selector value match", map[string]string{"ci.managed": "true"}, "ci.managed=true", true},
		{"selector value mismatch", map[string]string{"ci.managed": "false"}, "", false},
		{"bare key", map[string]string{testBare: ""}, testBare, true},
		{"label wins over selector", map[string]string{Label: testOwner, testBare: "1"}, testOwner, true},
	}
	for _, c := range cases {
		by, ok := p.Check(c.labels)
		if by != c.by || ok != c.ok {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", c.name, by, ok, c.by, c.ok)
		}
	}
}

func TestLabelsRoundTrip(t *testing.T) {
	in := map[string]string{"a": "b"}
	if got := DecodeLabels(EncodeLabels(in)); got["a"] != "b" || len(got) != 1 {
		t.Fatalf("round trip: %v", got)
	}
	if EncodeLabels(nil) != "{}" || DecodeLabels("garbage") != nil {
		t.Fatal("empty/garbage handling")
	}
}
