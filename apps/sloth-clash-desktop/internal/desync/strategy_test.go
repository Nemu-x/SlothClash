package desync_test

import (
	"testing"

	dlib "github.com/Nemu-x/desync"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/desync/desynctest"
)

func TestStrategiesDescribeTheCatalog(t *testing.T) {
	byName := map[string]desync.Strategy{}
	list := desync.Strategies(desynctest.Darwin())
	for _, s := range list {
		byName[s.Name] = s
	}
	if len(list) != 22+9 {
		t.Fatalf("%d strategies, want 22 TCP + 9 UDP", len(list))
	}
	for name, want := range map[string]struct {
		kind      string
		variant   int
		available bool
		tried     bool
	}{
		"tlsrec-1s":            {desync.KindTLSRec, 1, true, true},
		"tlsrec-3s":            {desync.KindTLSRec, 2, true, true},
		"tlsrec-multi":         {desync.KindTLSRec, 4, true, true},
		"random-tlsrec":        {desync.KindTLSRecRandom, 1, true, true},
		"split-1s":             {desync.KindSplit, 2, true, true},
		"tlsrec-1s-split-1":    {desync.KindTLSRecSplit, 1, true, true},
		"split-1s-wait-sent":   {desync.KindSplitWait, 1, false, false},
		"split-1s-wait-acked":  {desync.KindSplitWait, 2, true, true},
		"disoob-1s":            {desync.KindDisorderOOB, 1, true, true},
		"fake-ttl5":            {desync.KindFake, 2, false, false},
		"disorder-1-fake-ttl5": {desync.KindFakeSplit, 2, false, false},
		"udp-zero-ttl5-r12":    {desync.KindUDPZero, 5, true, false},
		"udp-quic-ttl3":        {desync.KindUDPQUIC, 1, true, false},
		"udp-zero-ttl5-r2":     {desync.KindUDPZero, 4, true, true},
	} {
		s := byName[name]
		if s.Kind != want.kind || s.Variant != want.variant || s.Available != want.available || s.Tried != want.tried {
			t.Errorf("%s = %+v, want %+v", name, s, want)
		}
		if !s.Available && s.Reason == "" {
			t.Errorf("%s: unavailable without a reason", name)
		}
	}
}

func TestCapabilitiesPerOS(t *testing.T) {
	supported := func(list []desync.Capability) map[string]bool {
		out := map[string]bool{}
		for _, c := range list {
			out[c.Method] = c.Supported
		}
		return out
	}
	mac := desync.Capabilities(desynctest.Darwin())
	if s := supported(mac); s["fake-tcp"] || s["split-wait-sent"] || !s["split-wait-acked"] || !s["fake-udp"] || !s["tlsrec"] {
		t.Fatalf("darwin %v", mac)
	}
	win := desync.Capabilities(desynctest.Windows())
	for _, c := range win {
		if !c.Supported {
			t.Fatalf("windows lacks %s", c.Method)
		}
		if c.Method == "split-wait-acked" && !c.Inferred {
			t.Fatal("windows split wait is counter based")
		}
	}
	for _, c := range desync.Capabilities(desynctest.Linux()) {
		if !c.Supported || c.Inferred {
			t.Fatalf("linux %+v", c)
		}
	}
}

// A method the library lists as Conditional (Android) is generated and
// reported as supported, decided per connection.
func TestConditionalMethodsAreAllowed(t *testing.T) {
	caps := desynctest.Linux()
	reason := "decided per socket"
	caps.FakeTCP, caps.Ack.Sent, caps.Disorder = false, false, dlib.DisorderCapability{}
	caps.Unsupported["fake-tcp"], caps.Unsupported["split-wait-sent"], caps.Unsupported["disorder"] = reason, reason, reason
	caps.Conditional = map[string]string{"fake-tcp": reason, "split-wait-sent": reason, "disorder": reason}
	if got, want := len(desync.Candidates("custom", caps)), len(desync.Candidates("custom", desynctest.Linux())); got != want {
		t.Fatalf("%d candidates with conditional methods, want %d", got, want)
	}
	for _, c := range desync.Capabilities(caps) {
		switch c.Method {
		case "fake-tcp", "split-wait-sent", "disorder":
			if !c.Supported || !c.Conditional || c.Reason != reason {
				t.Fatalf("%+v", c)
			}
		default:
			if c.Conditional {
				t.Fatalf("%+v", c)
			}
		}
	}
	caps.Conditional = nil
	if got := len(desync.Candidates("custom", caps)); got >= len(desync.Candidates("custom", desynctest.Linux())) {
		t.Fatal("unsupported methods generated without Conditional")
	}
}
