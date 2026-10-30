// Package desynctest holds the desync library's capability sets of the
// desktop systems, so that config generation can be tested for every OS on
// any OS. The values copy what desync.PlatformCapabilities reports on each
// (library at 673cbef: ack_*.go, hop_*.go, fake_*.go, oob_*.go,
// packet_*.go); TestPlatformMatchesFixture keeps this OS's copy honest.
package desynctest

import dlib "github.com/Nemu-x/desync"

// Windows is the capability set of Windows: every method, split-wait
// inferred from counters (SIO_TCP_INFO), no auto-ttl.
func Windows() dlib.Capabilities {
	return dlib.Capabilities{
		OS: "windows", FakeTCP: true, FakeUDP: true, TTLControl: true, OOB: true,
		Disorder: dlib.DisorderCapability{Supported: true, Mechanism: "SIO_TCP_INFO", Observed: true},
		Ack:      dlib.AckCapability{Mechanism: "SIO_TCP_INFO", Sent: true, Acked: true},
		Unsupported: map[string]string{
			"auto-ttl-ipv4": autoTTLReason("windows"),
			"auto-ttl-ipv6": autoTTLReason("windows"),
		},
	}
}

// Linux is the capability set of Linux: every method, auto-ttl for IPv6.
func Linux() dlib.Capabilities {
	return dlib.Capabilities{
		OS: "linux", FakeTCP: true, FakeUDP: true, TTLControl: true, OOB: true,
		Disorder: dlib.DisorderCapability{Supported: true, Mechanism: "SIOCOUTQNSD", Observed: true, Reliable: true},
		Ack:      dlib.AckCapability{Mechanism: "SIOCOUTQ/SIOCOUTQNSD", Sent: true, Acked: true, Reliable: true},
		AutoTTL:  dlib.AutoTTLCapability{Mechanism: "IPV6_RECVHOPLIMIT/IPV6_2292PKTOPTIONS", IPv6: true},
		Unsupported: map[string]string{
			"auto-ttl-ipv4": "Linux records no received TTL for an outgoing IPv4 TCP connection",
		},
	}
}

// Darwin is the capability set of macOS: no TCP fake, split-wait "acked"
// only, disorder inferred.
func Darwin() dlib.Capabilities {
	return dlib.Capabilities{
		OS: "darwin", FakeUDP: true, TTLControl: true, OOB: true,
		Disorder: dlib.DisorderCapability{Supported: true, Mechanism: "SO_NWRITE"},
		Ack:      dlib.AckCapability{Mechanism: "SO_NWRITE", Acked: true, Reliable: true},
		Unsupported: map[string]string{
			"fake-tcp":      "no splice: sendfile copies the data into the socket buffer, so a retransmission cannot carry the original bytes",
			"auto-ttl-ipv4": autoTTLReason("darwin"),
			"auto-ttl-ipv6": autoTTLReason("darwin"),
		},
	}
}

// ByOS returns the fixture of goos, and false for another system.
func ByOS(goos string) (dlib.Capabilities, bool) {
	switch goos {
	case "windows":
		return Windows(), true
	case "linux":
		return Linux(), true
	case "darwin":
		return Darwin(), true
	}
	return dlib.Capabilities{}, false
}

// autoTTLReason stands in for the library's wording, which the generated
// config never reads.
func autoTTLReason(goos string) string { return "auto-ttl is not implemented on " + goos }
