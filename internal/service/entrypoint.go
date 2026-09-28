package service

import (
	"fmt"
	"net/http"
	"os"
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
		port := "8080"
		if role == config.Worker {
			port = "8081"
		}
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		response, err := client.Get("http://127.0.0.1:" + port + "/livez")
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
