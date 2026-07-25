package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	// defaultDiscoveryDialTimeoutMS bounds the TCP dial to each candidate
	// host. It is the sweep's dominant cost: unused addresses in a /24 do not
	// refuse the connection, they black-hole it, so every one of them burns
	// the full timeout. Overridable via HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS
	// for links slower than a LAN, and for tests sweeping a reserved range
	// where nothing can answer by construction.
	defaultDiscoveryDialTimeoutMS = 600

	discoveryHTTPTimeout   = 2 * time.Second
	discoveryBudget        = 12 * time.Second
	discoveryConcurrency   = 64
	defaultSignalKScanPort = 3000
)

// discoveryDialTimeout resolves the per-host dial timeout from
// HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS, falling back to
// defaultDiscoveryDialTimeoutMS on an unset or invalid value (logged loudly
// rather than abandoning discovery over a malformed env var). Mirrors
// signalKReadTimeout.
func discoveryDialTimeout() time.Duration {
	raw := getEnv("HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS", strconv.Itoa(defaultDiscoveryDialTimeoutMS))
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		log.Printf("signalk discovery: invalid HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS %q, falling back to %dms", raw, defaultDiscoveryDialTimeoutMS)
		return defaultDiscoveryDialTimeoutMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// discoveredServer is one SignalK server found on the network, in the shape
// the settings UI needs to offer it: enough to identify the vessel, and
// enough to save the connection if the operator accepts.
type discoveredServer struct {
	Address    string `json:"address"`
	Port       int    `json:"port"`
	URL        string `json:"url"`
	VesselName string `json:"vessel_name"`
	Version    string `json:"version"`
}

// resolveDiscoveryNetwork decides which /24 to sweep.
//
// The backend runs in a bridge container (172.18.0.0/16), so it cannot see
// the LAN it is meant to search — the network has to be supplied. In priority
// order:
//
//  1. hint — the browser's own hostname, forwarded by the frontend. Covers a
//     first install, where the operator has browsed to the boat computer's
//     LAN address and nothing is configured yet.
//  2. configured — the currently-saved SignalK address. Covers "the server
//     moved", and still works when the dashboard is open at localhost.
//  3. HELMCENTRAL_DISCOVERY_SUBNET — escape hatch for topologies where
//     neither of the above lands on the right network.
//
// Anything outside RFC1918 is refused: a caller must not be able to point a
// port sweep at a public range. Failing to derive a network is an error
// rather than a guess — sweeping an arbitrary subnet would look like it
// worked while searching the wrong place.
func resolveDiscoveryNetwork(hint string, configured string) (*net.IPNet, error) {
	for _, candidate := range []string{hint, configured} {
		ip := net.ParseIP(strings.TrimSpace(candidate))
		if ip == nil || ip.To4() == nil {
			continue
		}
		if !ip.IsPrivate() {
			return nil, fmt.Errorf("%s is not a private address; discovery only searches local networks", ip)
		}
		return slashTwentyFour(ip), nil
	}

	if override := strings.TrimSpace(os.Getenv("HELMCENTRAL_DISCOVERY_SUBNET")); override != "" {
		_, network, err := net.ParseCIDR(override)
		if err != nil {
			return nil, fmt.Errorf("HELMCENTRAL_DISCOVERY_SUBNET %q is not a valid CIDR", override)
		}
		if !network.IP.IsPrivate() {
			return nil, fmt.Errorf("HELMCENTRAL_DISCOVERY_SUBNET %q is not a private network", override)
		}
		return network, nil
	}

	return nil, fmt.Errorf("cannot determine which network to scan")
}

func slashTwentyFour(ip net.IP) *net.IPNet {
	v4 := ip.To4()
	return &net.IPNet{
		IP:   net.IPv4(v4[0], v4[1], v4[2], 0).Mask(net.CIDRMask(24, 32)),
		Mask: net.CIDRMask(24, 32),
	}
}

// hostsInNetwork lists the usable host addresses of a /24, skipping the
// network and broadcast addresses.
func hostsInNetwork(network *net.IPNet) []string {
	base := network.IP.To4()
	if base == nil {
		return nil
	}

	hosts := make([]string, 0, 254)
	for octet := 1; octet <= 254; octet++ {
		hosts = append(hosts, net.IPv4(base[0], base[1], base[2], byte(octet)).String())
	}
	return hosts
}

// scanForSignalK probes every host/port pair and returns those that are
// actually running SignalK. A TCP connection alone is not enough to report a
// find: a router admin page on the same port would otherwise be offered to
// the operator as a vessel, so each hit must also answer /signalk.
func scanForSignalK(ctx context.Context, hosts []string, ports []int) []discoveredServer {
	var (
		mu    sync.Mutex
		found []discoveredServer
		wg    sync.WaitGroup
	)

	semaphore := make(chan struct{}, discoveryConcurrency)

	for _, host := range hosts {
		for _, port := range ports {
			if ctx.Err() != nil {
				break
			}

			wg.Add(1)
			go func(host string, port int) {
				defer wg.Done()

				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					return
				}

				if server, ok := probeSignalKServer(ctx, host, port); ok {
					mu.Lock()
					found = append(found, server)
					mu.Unlock()
				}
			}(host, port)
		}
	}

	wg.Wait()

	// Stable order so the UI doesn't reshuffle between scans.
	sort.Slice(found, func(i, j int) bool {
		if found[i].Address == found[j].Address {
			return found[i].Port < found[j].Port
		}
		return found[i].Address < found[j].Address
	})
	return found
}

// probeSignalKServer confirms a SignalK server is at host:port and describes
// it. The vessel name is the useful half — "a server responded" does not tell
// the operator whether it is their boat.
func probeSignalKServer(ctx context.Context, host string, port int) (discoveredServer, bool) {
	address := net.JoinHostPort(host, fmt.Sprint(port))

	dialer := net.Dialer{Timeout: discoveryDialTimeout()}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return discoveredServer{}, false
	}
	conn.Close()

	baseURL := buildSignalKURL(host, port)
	version, ok := signalKServerVersion(ctx, baseURL)
	if !ok {
		return discoveredServer{}, false
	}

	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	return discoveredServer{
		Address:    host,
		Port:       port,
		URL:        baseURL,
		VesselName: strings.TrimSpace(fetchSignalKSelfName(baseURL, vesselPath)),
		Version:    version,
	}, true
}

// signalKServerVersion fetches /signalk and reports the server version. A
// response that doesn't carry SignalK's endpoint descriptor isn't SignalK,
// whatever else it may be.
func signalKServerVersion(ctx context.Context, baseURL string) (string, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/signalk", nil)
	if err != nil {
		return "", false
	}

	client := &http.Client{Timeout: discoveryHTTPTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", false
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", false
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return "", false
	}

	var payload struct {
		Endpoints map[string]struct {
			Version string `json:"version"`
		} `json:"endpoints"`
		Server struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}

	if len(payload.Endpoints) == 0 {
		return "", false
	}

	if payload.Server.Version != "" {
		return payload.Server.Version, true
	}
	if v1, ok := payload.Endpoints["v1"]; ok && v1.Version != "" {
		return v1.Version, true
	}
	return "unknown", true
}

// discoverSignalKHandler backs the onboarding prompt: it sweeps the local
// network for SignalK servers and reports them. Like the connection probe, it
// is a pure read — the operator's confirmation is what saves anything, and
// that goes through POST /api/settings like every other write (ADR 0028).
func discoverSignalKHandler(c echo.Context) error {
	var req struct {
		Hint string `json:"hint"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	configuredAddress, configuredPort, err := loadSignalKSettings(settingsPath)
	if err != nil {
		configuredAddress = ""
		configuredPort = defaultSignalKScanPort
	}

	network, err := resolveDiscoveryNetwork(req.Hint, configuredAddress)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"field": "signalk.address",
			"error": err.Error(),
		})
	}

	ports := []int{defaultSignalKScanPort}
	if configuredPort > 0 && configuredPort != defaultSignalKScanPort {
		ports = append(ports, configuredPort)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), discoveryBudget)
	defer cancel()

	servers := scanForSignalK(ctx, hostsInNetwork(network), ports)
	if servers == nil {
		servers = []discoveredServer{}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"scanned_subnet": network.String(),
		"servers":        servers,
	})
}
