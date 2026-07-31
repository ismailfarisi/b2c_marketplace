package config

import (
	"fmt"
	"net"
	"strings"
)

// AutoHost is the sentinel a development config uses in place of a hostname it
// cannot know in advance.
//
// # The problem it solves
//
// In development, MinIO has to be reachable from three places that disagree
// about what "this machine" is called:
//
//	the API process   localhost:9000
//	a browser         localhost:9000
//	an emulator       10.0.2.2:9000, which is its own alias for the host
//	a real device     the machine's address on the WiFi
//
// Presigned URLs and poster URLs are rendered on a *device*, so they must name
// something the device can reach — which rules out localhost — and the web
// admin has to reach the same objects, which rules out 10.0.2.2. The only
// address that works everywhere is the machine's LAN address.
//
// Writing that address into .env works until the DHCP lease changes, at which
// point every image in the app 404s and every upload fails, with no error that
// mentions an IP. Resolving it at boot removes the class.
//
// # Why this is development-only by construction
//
// A production deployment names a real hostname — a CloudFront distribution, an
// S3 endpoint — and never sees this code. `Load` refuses the sentinel when
// APP_ENV is production rather than resolving it, because a public URL derived
// from a container's private address is a subtly broken deployment rather than
// an obviously broken one.
const AutoHost = "auto"

// resolveAuto replaces the host portion of a URL with this machine's LAN
// address.
//
// Takes the address the OS would use to reach the outside world, found by
// opening a UDP socket towards a public address. No packet is sent — UDP
// "connect" only fixes the destination for later writes, which is enough for
// the kernel to pick a source interface. It works with no network too, in the
// sense that it fails cleanly rather than hanging, which enumerating interfaces
// and guessing does not.
//
// Enumerating was the first attempt and it is worse: this machine has a WSL
// virtual adapter on 172.18.224.1, and picking the wrong one produces an
// address that resolves, accepts a connection from the host, and is invisible
// to every phone on the WiFi.
func resolveAuto(raw string) (string, error) {
	if !strings.Contains(raw, AutoHost) {
		return raw, nil
	}
	ip, err := outboundIP()
	if err != nil {
		return "", err
	}
	return strings.Replace(raw, AutoHost, ip, 1), nil
}

func outboundIP() (string, error) {
	// 192.0.2.1 is TEST-NET-1 (RFC 5737) — reserved for documentation and
	// guaranteed never to be routed. A real address would work equally well and
	// would also mean this function quietly depends on someone else's server
	// existing.
	conn, err := net.Dial("udp", "192.0.2.1:1")
	if err != nil {
		return "", fmt.Errorf("config: cannot determine this machine's address for %q: %w", AutoHost, err)
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("config: unexpected local address type")
	}
	return addr.IP.String(), nil
}
