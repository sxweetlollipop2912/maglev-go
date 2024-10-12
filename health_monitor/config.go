package health_monitor

import (
	"github.com/rs/zerolog"
	"net/url"
	"time"
)

type Config struct {
	// Backends is the list of backends that would be added to the health monitor during startup.
	Backends []*BackendConfig `mapstructure:"backends"`
	// UnhealthyThreshold is the number of consecutive health checks that must fail
	// before a backend is considered unhealthy.
	UnhealthyThreshold int `mapstructure:"unhealthy_threshold" default:"3"`
	// HealthyThreshold is the number of consecutive health checks that must pass
	// before a backend is considered healthy.
	HealthyThreshold int `mapstructure:"healthy_threshold" default:"2"`
	// Interval is the time between health checks.
	Interval time.Duration `mapstructure:"interval" default:"30s"`
	// Timeout is the time to wait for a response from the backend before considering
	// it unhealthy.
	// If the timeout is greater than 2/3 the interval, the timeout is set to 2/3 the interval
	// to avoid unnecessary waiting & deadlocks.
	Timeout time.Duration `mapstructure:"timeout" default:"5s"`
	// AcceptStatusCodes is the list of status code regex patterns to accept as healthy.
	AcceptStatusCodes []string `mapstructure:"accept_status_codes" default:"[\"2.+\"]"`
	// HealthyInitially is the initial state of the backend.
	// If true, the backend is assumed to be healthy when first added.
	HealthyInitially bool `mapstructure:"healthy_initially" default:"true"`
	// TODO: SSL configuration

	// Runtime configuration
	// EnableHealthyChannel enables sending to channel when a new backend becomes healthy.
	EnableHealthyChannel bool `mapstructure:"send_new_healthy" default:"false"`
	// EnableUnhealthyChannel enables sending to channel when a new backend becomes unhealthy.
	EnableUnhealthyChannel bool `mapstructure:"send_new_unhealthy" default:"false"`

	logger zerolog.Logger
}

type BackendConfig struct {
	// Name is the name of this backend. Must be unique.
	Name string `mapstructure:"name"`
	// Url is the URL with healthcheck path of this backend.
	Url url.URL `mapstructure:"url"`
	// Protocol is the protocol to use for health checks. Default is "http".
	Protocol Protocol `mapstructure:"protocol" default:"http"`
	// Timeout overrides the global timeout for this backend.
	// If the timeout is greater than 2/3 the global interval, the timeout is set to 2/3 the interval
	// to avoid unnecessary waiting & deadlocks.
	Timeout time.Duration `mapstructure:"timeout"`
	// AcceptStatusCodes overrides the global accept status codes for this backend.
	AcceptStatusCodes []string `mapstructure:"accept_status_codes"`
	// UnhealthyThreshold overrides the global unhealthy threshold for this backend.
	UnhealthyThreshold int `mapstructure:"unhealthy_threshold"`
	// HealthyThreshold overrides the global healthy threshold for this backend.
	HealthyThreshold int `mapstructure:"healthy_threshold"`
}

type Protocol string

const (
	HTTP  Protocol = "http"
	HTTPS Protocol = "https"
	TCP   Protocol = "tcp"
	ICMP  Protocol = "icmp"
)
