package sharelink

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const fmVlessBase = "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=tls"

func fmLink(fm string) string { return fmVlessBase + "&fm=" + url.QueryEscape(fm) + "#n" }

func fragmentOptsOf(t *testing.T, link string) (map[string]any, bool) {
	t.Helper()
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", link, err)
	}
	v, ok := p["fragment-opts"]
	if !ok {
		return nil, false
	}
	m, isMap := v.(map[string]any)
	if !isMap {
		t.Fatalf("fragment-opts is %T, want map[string]any", v)
	}
	return m, true
}

// The vectors of core patch 0032 (common/convert/fragment_test.go).
func TestFinalMaskFragmentCoreVectors(t *testing.T) {
	fm := `{"tcp":[{"type":"header-custom","settings":{}},{"type":"fragment","settings":{"packets":"tlshello","length":"10-20","delay":0,"maxSplit":"5-9"}}]}`
	link := "vless://11111111-2222-3333-4444-555555555555@example.com:2053?security=reality&pbk=key&sid=ab&sni=example.com&fp=chrome&type=tcp&fm=" + url.QueryEscape(fm) + "#n"
	got, ok := fragmentOptsOf(t, link)
	want := map[string]any{"packets": "tlshello", "length": "10-20", "delay": "0", "max-split": "5-9"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("fragment-opts = %#v, want %#v", got, want)
	}

	for _, fm := range []string{"", "not json", `{"tcp":[{"type":"header-custom"}]}`, `{"udp":[{"type":"fragment"}]}`,
		`{"tcp":[{"type":"fragment","settings":{"packets":"abc"}}]}`, `{"tcp":[{"type":"fragment","settings":{"delay":-1}}]}`,
		`{"tcp":[{"type":"fragment","settings":{"delay":"0-9223372036854775807"}}]}`} {
		if got, ok := fragmentOptsOf(t, fmLink(fm)); ok {
			t.Errorf("fm=%q: fragment-opts = %#v, want none", fm, got)
		}
	}

	fm = `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","lengths":["10-20","30"],"delays":[0]}}]}`
	got, ok = fragmentOptsOf(t, fmLink(fm))
	want = map[string]any{"packets": "tlshello", "length": "10-20", "delay": "0"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("lists: fragment-opts = %#v, want %#v", got, want)
	}
}

func TestFinalMaskFragmentMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		fm   string
		want map[string]any // nil: no fragment-opts
	}{
		{"raw ranges", `{"tcp":[{"type":"fragment","settings":{"packets":"1-3","length":50,"delay":"5-10","maxSplit":4}}]}`,
			map[string]any{"packets": "1-3", "length": "50", "delay": "5-10", "max-split": "4"}},
		{"reversed range kept as given", `{"tcp":[{"type":"fragment","settings":{"length":"20-10"}}]}`,
			map[string]any{"length": "20-10"}},
		{"no settings defaults to tlshello", `{"tcp":[{"type":"fragment"}]}`,
			map[string]any{"packets": "tlshello"}},
		{"empty settings defaults to tlshello", `{"tcp":[{"type":"fragment","settings":{}}]}`,
			map[string]any{"packets": "tlshello"}},
		{"unknown keys ignored", `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","interval":"1-2","foo":true}}],"udp":[{"type":"noise"}],"x":1}`,
			map[string]any{"packets": "tlshello"}},
		{"only unknown keys defaults to tlshello", `{"tcp":[{"type":"fragment","settings":{"interval":"1-2"}}]}`,
			map[string]any{"packets": "tlshello"}},
		{"type is case-insensitive", `{"tcp":[{"type":"Fragment","settings":{"packets":"TLSHello"}}]}`,
			map[string]any{"packets": "TLSHello"}},
		{"json field names are case-insensitive", `{"TCP":[{"Type":"fragment","Settings":{"length":"5"}}]}`,
			map[string]any{"length": "5"}},
		{"settings keys are case-sensitive", `{"tcp":[{"type":"fragment","settings":{"Length":"0"}}]}`,
			map[string]any{"packets": "tlshello"}},
		{"numbers truncate", `{"tcp":[{"type":"fragment","settings":{"length":5.9,"delay":1e1}}]}`,
			map[string]any{"length": "5", "delay": "10"}},
		{"bool and null values ignored", `{"tcp":[{"type":"fragment","settings":{"packets":true,"length":null,"delay":"1"}}]}`,
			map[string]any{"delay": "1"}},
		{"explicit value wins over list", `{"tcp":[{"type":"fragment","settings":{"length":"7","lengths":["10-20"],"delays":["3-4"]}}]}`,
			map[string]any{"length": "7", "delay": "3-4"}},
		{"empty or non-list lists ignored", `{"tcp":[{"type":"fragment","settings":{"lengths":[],"delays":"5"}}]}`,
			map[string]any{"packets": "tlshello"}},
		{"empty packets string kept", `{"tcp":[{"type":"fragment","settings":{"packets":""}}]}`,
			map[string]any{"packets": ""}},
		{"values with spaces pass the check as is", `{"tcp":[{"type":"fragment","settings":{"length":" 10 - 20 "}}]}`,
			map[string]any{"length": " 10 - 20 "}},
		{"second mask after a non-fragment one", `{"tcp":[{"type":"noise"},{"type":"fragment","settings":{"packets":"2"}}]}`,
			map[string]any{"packets": "2"}},

		{"malformed json", `{"tcp":[{"type":"fragment"`, nil},
		{"tcp not a list", `{"tcp":{"type":"fragment"}}`, nil},
		{"settings not an object", `{"tcp":[{"type":"fragment","settings":[1]}]}`, nil},
		{"type not a string", `{"tcp":[{"type":1}]}`, nil},
		{"json null", `null`, nil},
		{"empty object", `{}`, nil},
		{"packets 0", `{"tcp":[{"type":"fragment","settings":{"packets":"0-3"}}]}`, nil},
		{"packets word", `{"tcp":[{"type":"fragment","settings":{"packets":"first"}}]}`, nil},
		{"length from 0", `{"tcp":[{"type":"fragment","settings":{"length":"0-5"}}]}`, nil},
		{"length number 0", `{"tcp":[{"type":"fragment","settings":{"length":0}}]}`, nil},
		{"length list from 0", `{"tcp":[{"type":"fragment","settings":{"lengths":["0-5"]}}]}`, nil},
		{"negative max-split", `{"tcp":[{"type":"fragment","settings":{"maxSplit":"-2"}}]}`, nil},
		{"range above int32", `{"tcp":[{"type":"fragment","settings":{"length":"1-2147483648"}}]}`, nil},
		{"huge number", `{"tcp":[{"type":"fragment","settings":{"delay":1e300}}]}`, nil},
		{"bad range text", `{"tcp":[{"type":"fragment","settings":{"delay":"1-x"}}]}`, nil},
		{"only the first fragment mask counts", `{"tcp":[{"type":"fragment","settings":{"packets":"abc"}},{"type":"fragment","settings":{"packets":"1"}}]}`, nil},
		{"fragment mask under udp only", `{"udp":[{"type":"fragment","settings":{"packets":"tlshello"}}]}`, nil},
	} {
		got, ok := fragmentOptsOf(t, fmLink(tc.fm))
		switch {
		case tc.want == nil && ok:
			t.Errorf("%s: fragment-opts = %#v, want none", tc.name, got)
		case tc.want != nil && !reflect.DeepEqual(got, tc.want):
			t.Errorf("%s: fragment-opts = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestFinalMaskFragmentEncodingsAndScope(t *testing.T) {
	fm := `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"10-20"}}]}`
	want := map[string]any{"packets": "tlshello", "length": "10-20"}

	// absent: no key at all, and a bad fm never fails the link itself
	if got, ok := fragmentOptsOf(t, fmVlessBase+"#n"); ok {
		t.Errorf("no fm: fragment-opts = %#v", got)
	}
	if got, ok := fragmentOptsOf(t, fmVlessBase+"&fm=#n"); ok {
		t.Errorf("empty fm: fragment-opts = %#v", got)
	}

	// percent-encoded (%20 / +), and the JSON pasted unescaped: the query
	// decoder yields the same JSON, as in the core
	for name, link := range map[string]string{
		"query escape": fmLink(fm),
		"path escape":  fmVlessBase + "&fm=" + url.PathEscape(fm) + "#n",
		"unescaped":    fmVlessBase + "&fm=" + fm + "#n",
		"spaces as +":  fmVlessBase + "&fm=" + url.QueryEscape(strings.ReplaceAll(fm, ",", ", ")) + "#n",
	} {
		if got, ok := fragmentOptsOf(t, link); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: fragment-opts = %#v, want %#v", name, got, want)
		}
	}

	// base64 is not an fm form the core reads: dropped, link kept
	if got, ok := fragmentOptsOf(t, fmVlessBase+"&fm="+url.QueryEscape(b64(fm))+"#n"); ok {
		t.Errorf("base64 fm: fragment-opts = %#v, want none", got)
	}

	// any vless network / security, like the core's handleVShareLink
	for _, q := range []string{"type=ws&path=/w", "type=grpc&serviceName=s", "type=xhttp&path=/x", "security=none"} {
		link := "vless://11111111-2222-3333-4444-555555555555@example.com:443?" + q + "&fm=" + url.QueryEscape(fm) + "#n"
		if got, ok := fragmentOptsOf(t, link); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: fragment-opts = %#v, want %#v", q, got, want)
		}
	}

	// the core maps fm only on links it parses with handleVShareLink (vless
	// and the VMess URL form, which this parser does not read); trojan,
	// base64 vmess and the rest never carry it
	for _, link := range []string{
		"trojan://pw@example.com:443?sni=example.com&fm=" + url.QueryEscape(fm) + "#t",
		"vmess://" + b64(`{"v":"2","ps":"v","add":"example.com","port":"443","id":"u","net":"tcp","tls":"tls","fm":`+strconv.Quote(fm)+`}`),
		"hysteria2://pw@example.com:443?sni=example.com&fm=" + url.QueryEscape(fm) + "#h",
	} {
		if got, ok := fragmentOptsOf(t, link); ok {
			t.Errorf("%s: fragment-opts = %#v, want none", link[:8], got)
		}
	}
}
