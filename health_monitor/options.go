package health_monitor

import (
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	iviper "maglev-go/x/viper"
	"time"
)

type Option func(*Config) error

func LoadConfig(v *viper.Viper) Option {
	return func(c *Config) error {
		return iviper.Unmarshal(v, c)
	}
}

func WithConfig(cfg *Config) Option {
	return func(c *Config) error {
		*c = *cfg
		return nil
	}
}

func WithUnhealthyThreshold(threshold int) Option {
	return func(c *Config) error {
		c.UnhealthyThreshold = threshold
		return nil
	}
}

func WithHealthyThreshold(threshold int) Option {
	return func(c *Config) error {
		c.HealthyThreshold = threshold
		return nil
	}
}

func WithCheckInterval(interval time.Duration) Option {
	return func(c *Config) error {
		c.Interval = interval
		return nil
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) error {
		c.Timeout = timeout
		return nil
	}
}

func WithHttpPath(path string) Option {
	return func(c *Config) error {
		c.HttpPath = path
		return nil
	}
}

func WithProtocol(protocol Protocol) Option {
	return func(c *Config) error {
		c.Protocol = protocol
		return nil
	}
}

func WithAcceptStatusCodes(codePatterns ...string) Option {
	return func(c *Config) error {
		c.AcceptStatusCodes = codePatterns
		return nil
	}
}

func WithLogLevel(level zerolog.Level) Option {
	return func(c *Config) error {
		c.logger = c.logger.Level(level)
		return nil
	}
}

func EnableHealthyChannel() Option {
	return func(c *Config) error {
		c.EnableHealthyChannel = true
		return nil
	}
}

func EnableUnhealthyChannel() Option {
	return func(c *Config) error {
		c.EnableUnhealthyChannel = true
		return nil
	}
}
