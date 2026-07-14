package network

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	ipv4HazipURL = "https://ipv4.icanhazip.com/"
	ifconfigURL  = "https://ifconfig.me/ip"
)

const publicIPTimeout = 10 * time.Second

// PublicIP returns the host's public IPv4 address.
// It honors the AXON_PUBLIC_IP environment variable to skip external calls.
// Otherwise it queries ipv4.icanhazip.com, falling back to ifconfig.me/ip.
func PublicIP(client *http.Client) (string, error) {
	if env := os.Getenv("AXON_PUBLIC_IP"); env != "" {
		return strings.TrimSpace(env), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), publicIPTimeout)
	defer cancel()

	ip, err := fetchPublicIP(ctx, client, ipv4HazipURL)
	if err != nil {
		ip, err = fetchPublicIP(ctx, client, ifconfigURL)
	}
	return ip, err
}

func fetchPublicIP(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public IP lookup %s returned status %d", url, resp.StatusCode)
	}

	lim := io.LimitReader(resp.Body, 64)
	body, err := io.ReadAll(lim)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}
