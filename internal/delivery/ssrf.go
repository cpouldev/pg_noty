package delivery

import (
	"fmt"
	"net"
	"strings"
	"syscall"
)

// blockedRanges is deliberately explicit rather than relying on the address
// string or on IP.IsPrivate alone.  The ranges include both address families,
// metadata/link-local space, and multicast.  IPv4-mapped IPv6 values are
// normalised to their four-byte form before these networks are checked.
// 64:ff9b::/96 and 2002::/16 are listed explicitly because To4 does not reduce them: NAT64 and 6to4
// embed a v4 address without the ::ffff: prefix, so 2002:7f00:0001:: would otherwise reach 127.0.0.1
// looking like a public v6 address. 100.64.0.0/10 is CGNAT, reachable inside several cloud
// providers; a deployment that needs it names it in worker.allowed_destination_cidrs.
var blockedRanges = mustNetworks([]string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b::/96", "2002::/16", "fc00::/7",
	"fe80::/10", "ff00::/8",
})

func mustNetworks(values []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		result = append(result, network)
	}
	return result
}

// ParseAllowedDestinations turns worker.allowed_destination_cidrs into the networks the guard will
// admit, refusing the whole set on the first unparseable entry: a silently dropped exemption reads
// as a working guard right up to the delivery that needed it.
func ParseAllowedDestinations(values []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("worker.allowed_destination_cidrs entry %q is not a CIDR: %w", value, err)
		}
		networks = append(networks, network)
	}
	return networks, nil
}

// blockedAddress reports whether ip belongs to a non-routable or otherwise unsafe destination
// range. An exempt network wins over blockedRanges -- that is what the allow-list is for, and why
// its entries are exact prefixes rather than a blanket "permit private space".
func blockedAddress(ip net.IP, exempt []*net.IPNet) bool {
	if mapped := ip.To4(); mapped != nil {
		ip = mapped
	}
	for _, network := range exempt {
		if network.Contains(ip) {
			return false
		}
	}
	for _, network := range blockedRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfSafeControl is installed on every listener's net.Dialer.  net/http has
// already resolved the host when this callback runs, and invokes it immediately
// before the connect syscall.  Checking this address closes the DNS-rebinding
// gap that a hostname-only validation leaves open.
func ssrfSafeControl(network string, address string, conn syscall.RawConn) error {
	return ssrfControlAllowing(nil)(network, address, conn)
}

// ssrfControlAllowing is the same guard with an operator's exemptions applied.
func ssrfControlAllowing(exempt []*net.IPNet) func(string, string, syscall.RawConn) error {
	return func(_ string, address string, _ syscall.RawConn) error {
		return refuseUnsafeAddress(address, exempt)
	}
}

func refuseUnsafeAddress(address string, exempt []*net.IPNet) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse resolved address %q: %w", address, err)
	}
	host = strings.Trim(host, "[]")
	if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("resolved address %q is not an IP", address)
	}
	if blockedAddress(ip, exempt) {
		return fmt.Errorf("refusing connection to blocked address %s", ip)
	}
	return nil
}
