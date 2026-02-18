// Package metrics provides a DogStatsD client for custom application metrics.
package metrics

import (
	"fmt"
	"os"

	"github.com/DataDog/datadog-go/v5/statsd"
)

// Client is the global DogStatsD metrics client.
// It is nil-safe: callers can use helpers without checking for nil.
var Client *statsd.Client

// Init initializes the DogStatsD client.
// Uses DD_AGENT_HOST and DD_DOGSTATSD_PORT env vars (set in K8s manifests).
// Returns nil if the agent host is not configured (Datadog disabled).
func Init() error {
	host := os.Getenv("DD_AGENT_HOST")
	if host == "" {
		// Datadog not configured, metrics are no-ops
		return nil
	}
	port := os.Getenv("DD_DOGSTATSD_PORT")
	if port == "" {
		port = "8125"
	}

	var err error
	Client, err = statsd.New(fmt.Sprintf("%s:%s", host, port),
		statsd.WithNamespace("netclode."),
		statsd.WithTags([]string{
			"service:" + os.Getenv("DD_SERVICE"),
			"env:" + os.Getenv("DD_ENV"),
		}),
	)
	return err
}

// Close flushes and closes the metrics client.
func Close() {
	if Client != nil {
		Client.Close()
	}
}

// Incr increments a counter. No-op if Client is nil.
func Incr(name string, tags []string) {
	if Client != nil {
		Client.Incr(name, tags, 1)
	}
}

// Gauge sets a gauge value. No-op if Client is nil.
func Gauge(name string, value float64, tags []string) {
	if Client != nil {
		Client.Gauge(name, value, tags, 1)
	}
}

// Distribution records a distribution value. No-op if Client is nil.
func Distribution(name string, value float64, tags []string) {
	if Client != nil {
		Client.Distribution(name, value, tags, 1)
	}
}
