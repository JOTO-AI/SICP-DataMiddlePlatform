package service

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
)

// Entrypoint keeps deployment probes in the service binary. It exposes no
// management commands and never starts a task when checking configuration.
func Entrypoint(role config.Role, args []string) int {
	if len(args) == 0 {
		return Main(role)
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "unexpected service arguments")
		return 1
	}
	switch args[0] {
	case "--check-config":
		if _, err := config.LoadRuntime(role, os.LookupEnv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "--healthcheck":
		endpoint, err := healthcheckURL(role, os.LookupEnv)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		response, err := client.Get(endpoint)
		if err != nil {
			return 1
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return 1
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown service option")
		return 1
	}
}

// Probe the configured listener without loading database/source credentials or
// checking readiness: initialization and recovery are normal live states.
func healthcheckURL(role config.Role, lookup func(string) (string, bool)) (string, error) {
	address := "127.0.0.1:8080"
	switch role {
	case config.Query:
	case config.Worker:
		address = "127.0.0.1:8081"
	default:
		return "", fmt.Errorf("SERVICE_ROLE: invalid role")
	}
	if value, ok := lookup("HTTP_ADDRESS"); ok {
		address = value
	}
	host, port, err := net.SplitHostPort(address)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("HTTP_ADDRESS: invalid healthcheck address")
	}
	if host == "" {
		host = "127.0.0.1"
	} else if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	}
	endpoint := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/livez"}
	return endpoint.String(), nil
}
