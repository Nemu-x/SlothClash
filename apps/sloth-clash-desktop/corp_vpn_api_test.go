package main

import (
	"reflect"
	"testing"
)

func TestParseCorpEnvelope_CertTrustIsNotError(t *testing.T) {
	body := []byte(`{"code":1,"message":"trust required","data":{"connected":false,"needs_cert_trust":true,"servercert_sha256":"sha256:abcd"}}`)
	status, err := parseCorpEnvelope(503, body)
	if err != nil {
		t.Fatalf("cert-trust must not be an error, got %v", err)
	}
	if !status.NeedsCertTrust || status.ServercertSHA256 != "sha256:abcd" {
		t.Fatalf("cert-trust not surfaced: %#v", status)
	}
}

func TestParseCorpEnvelope_HTTPErrorSurfaces(t *testing.T) {
	body := []byte(`{"code":1,"message":"auth failed","data":null}`)
	_, err := parseCorpEnvelope(503, body)
	if err == nil {
		t.Fatal("expected an error for a real service failure")
	}
}

func TestParseCorpEnvelope_SuccessMapsData(t *testing.T) {
	body := []byte(`{"code":0,"message":"Success","data":{"connected":true,"routes":["10.0.0.0/8"],"dns_servers":["10.16.32.100"],"dns_domains":["corp.example"],"full_tunnel":false}}`)
	status, err := parseCorpEnvelope(200, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Connected || !reflect.DeepEqual(status.Routes, []string{"10.0.0.0/8"}) {
		t.Fatalf("bad mapping: %#v", status)
	}
	if !status.Supported {
		t.Fatalf("supported should be true when data present")
	}
}

func TestCorpSplitFromStatus(t *testing.T) {
	cases := []struct {
		name string
		in   CorpVpnStatus
		want corpVpnSplit
	}{
		{
			name: "connected split",
			in:   CorpVpnStatus{Connected: true, Routes: []string{"10.0.0.0/8"}, DNSServers: []string{"10.16.32.100"}, DNSDomains: []string{"corp"}},
			want: corpVpnSplit{Routes: []string{"10.0.0.0/8"}, DNSServers: []string{"10.16.32.100"}, DNSDomains: []string{"corp"}},
		},
		{
			name: "full tunnel → inactive",
			in:   CorpVpnStatus{Connected: true, FullTunnel: true, Routes: []string{"0.0.0.0/0"}},
			want: corpVpnSplit{},
		},
		{
			name: "cert pending → inactive",
			in:   CorpVpnStatus{NeedsCertTrust: true, Routes: []string{"10.0.0.0/8"}},
			want: corpVpnSplit{},
		},
		{
			name: "disconnected → inactive",
			in:   CorpVpnStatus{Connected: false, Routes: []string{"10.0.0.0/8"}},
			want: corpVpnSplit{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := corpSplitFromStatus(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
			if got.active() != (len(tc.want.Routes) > 0) {
				t.Fatalf("active() mismatch for %#v", got)
			}
		})
	}
}
