package health_monitor

import (
	"context"
	"fmt"
	"github.com/creasty/defaults"
	"github.com/rs/zerolog"
	ilog "maglev-go/x/log"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrChannelNotEnabled = fmt.Errorf("channel not enabled")
)

type HealthMonitor interface {
	// Start starts the health monitor non-blocking.
	// Returns an error if there is a problem starting the health monitor.
	// To stop the health monitor, call Stop.
	Start() (err error)
	// UnhealthyChan returns a channel that receives newly unhealthy backends.
	// Backends that are removed from the health monitor are also sent to this channel,
	// but its timestamp is nil to indicate that the backend is indefinitely unreachable.
	// If initial health is set to "Unhealthy", backends that are newly added are
	// also sent to this channel.
	UnhealthyChan() (<-chan *HealthNoti, error)
	// HealthyChan returns a channel that receives newly healthy backends.
	// If initial health is set to "Healthy", backends that are newly added are
	// also sent to this channel.
	HealthyChan() (<-chan *HealthNoti, error)
	// Stop stops the health monitor.
	Stop()
	// IsHealthy returns true if the given backend is healthy.
	IsHealthy(name string) bool
	// Add adds the given backends to the health monitor.
	// If a backend already exists, it is ignored.
	// If returning an error, the health monitor is unchanged.
	Add(backends ...*BackendConfig) error
	// Remove removes the given backends from the health monitor.
	Remove(backends ...string)
	// Size returns the number of backends in the health monitor.
	Size() int
	// LastCheckedAt returns the last time the health monitor checked the backends.
	LastCheckedAt() time.Time
	// NextCheckAt returns the time the health monitor will check the backends next.
	NextCheckAt() time.Time
}

type healthMonitorImpl struct {
	cfg         Config
	backends    map[string]*Backend
	backendsMtx *sync.RWMutex
	lastChecked time.Time
	outputChans outputChannels

	ctx           context.Context
	cancelCtx     context.CancelFunc
	tickerStopped chan struct{}
}

type outputChannels struct {
	enableHealthyChan   bool
	unhealthyChan       chan *HealthNoti
	enableUnhealthyChan bool
	healthyChan         chan *HealthNoti
}

// NewHealthMonitor creates a new HealthMonitor.
func NewHealthMonitor(ctx context.Context, opts ...Option) (HealthMonitor, error) {
	cfg := Config{
		logger: ilog.Logger.
			With().Str("component", "health_monitor").
			Logger().Level(zerolog.InfoLevel),
	}
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if err := defaults.Set(&cfg); err != nil {
		return nil, err
	}

	if cfg.Timeout > cfg.Interval*2/3 {
		cfg.logger.Warn().
			Dur("timeout", cfg.Timeout).
			Dur("interval", cfg.Interval).
			Msg("Connection timeout is greater than 2/3 interval. Setting timeout to 2/3 interval.")
		cfg.Timeout = cfg.Interval * 2 / 3
	}
	for i := range cfg.Backends {
		be := cfg.Backends[i]
		if be.Timeout > cfg.Interval*2/3 {
			cfg.logger.Warn().
				Str("backend", be.Name).
				Dur("timeout", be.Timeout).
				Dur("interval", cfg.Interval).
				Msg("Connection timeout of backend is greater than 2/3 interval. Setting timeout to 2/3 interval.")
			be.Timeout = cfg.Interval * 2 / 3
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	return &healthMonitorImpl{
		cfg:           cfg,
		backends:      make(map[string]*Backend),
		backendsMtx:   &sync.RWMutex{},
		outputChans:   newOutputChannels(cfg.EnableHealthyChannel, cfg.EnableUnhealthyChannel),
		ctx:           ctx,
		cancelCtx:     cancel,
		tickerStopped: make(chan struct{}),
	}, nil
}

func (h *healthMonitorImpl) Start() (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			if err, ok = r.(error); !ok {
				err = fmt.Errorf("panic: %v", r)
			}
			h.cfg.logger.Err(err).Msg("Health monitor failed to start")
		}
	}()

	if len(h.cfg.Backends) > 0 {
		h.cfg.logger.Info().
			Int("backends", len(h.cfg.Backends)).
			Msgf("Adding %d backends...", len(h.cfg.Backends))

		beCfgs := make([]*BackendConfig, 0, len(h.cfg.Backends))
		for _, beCfg := range h.cfg.Backends {
			beCfgs = append(beCfgs, beCfg)
		}

		if err = h.Add(beCfgs...); err != nil {
			return err
		}
	}

	h.cfg.logger.Info().
		Interface("config", h.cfg).
		Msg("Starting health monitor...")

	go func() {
		defer close(h.tickerStopped)

		ticker := time.NewTicker(h.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-ticker.C:
				h.lastChecked = time.Now()

				h.backendsMtx.Lock()
				var wg sync.WaitGroup

				wg.Add(len(h.backends))
				for _, backend := range h.backends {
					go func(backend *Backend) {
						defer wg.Done()
						if healthy, newly := h.healthcheck(backend); newly {
							if healthy {
								h.outputChans.sendHealthy(backend.toNoti())
							} else {
								h.outputChans.sendUnhealthy(backend.toNoti())
							}
						}
					}(backend)
				}

				wg.Wait()
				h.backendsMtx.Unlock()
			}
		}
	}()

	return nil
}

func (h *healthMonitorImpl) Stop() {
	h.cfg.logger.Info().Msg("Stopping health monitor...")
	h.cancelCtx()
	<-h.tickerStopped
	h.outputChans.close()
}

func (h *healthMonitorImpl) IsHealthy(name string) bool {
	h.backendsMtx.RLock()
	defer h.backendsMtx.RUnlock()

	if backend, ok := h.backends[name]; ok {
		return backend.healthy
	}
	return false
}

func (h *healthMonitorImpl) Add(beConfigs ...*BackendConfig) error {
	for i := range beConfigs {
		if err := defaults.Set(beConfigs[i]); err != nil {
			return err
		}

		// name and url are required
		if beConfigs[i].Name == "" {
			return fmt.Errorf("backend name is required")
		}
		if beConfigs[i].Url.String() == "" {
			return fmt.Errorf("backend URL is required")
		}

		// set global defaults if not set
		if beConfigs[i].Timeout > h.cfg.Interval*2/3 {
			h.cfg.logger.Warn().
				Str("backend", beConfigs[i].Name).
				Dur("timeout", beConfigs[i].Timeout).
				Dur("interval", h.cfg.Interval).
				Msg("Connection timeout of backend is greater than 2/3 interval. Setting timeout to 2/3 interval.")
			beConfigs[i].Timeout = h.cfg.Interval * 2 / 3
		} else if beConfigs[i].Timeout == 0 {
			beConfigs[i].Timeout = h.cfg.Timeout
		}
		if beConfigs[i].AcceptStatusCodes == nil {
			beConfigs[i].AcceptStatusCodes = h.cfg.AcceptStatusCodes
		}
		if beConfigs[i].UnhealthyThreshold == 0 {
			beConfigs[i].UnhealthyThreshold = h.cfg.UnhealthyThreshold
		}
		if beConfigs[i].HealthyThreshold == 0 {
			beConfigs[i].HealthyThreshold = h.cfg.HealthyThreshold
		}
	}

	h.backendsMtx.Lock()
	defer h.backendsMtx.Unlock()

	for i := range beConfigs {
		beCfg := beConfigs[i]
		if backend, ok := h.backends[beCfg.Name]; ok {
			h.cfg.logger.Warn().
				Str("beCfg", backend.Cfg.Name).
				Str("url", backend.Cfg.Url.String()).
				Bool("healthy", backend.healthy).
				Msg("Backend already exists")
			continue
		}

		// Add BE state to the health monitor
		be := &Backend{
			Cfg:     beCfg,
			healthy: h.cfg.HealthyInitially,
		}
		h.backends[be.Cfg.Name] = be

		if be.healthy {
			h.outputChans.sendHealthy(be.toNoti())
		} else {
			h.outputChans.sendUnhealthy(be.toNoti())
		}
	}

	return nil
}

func (h *healthMonitorImpl) Remove(backends ...string) {
	h.backendsMtx.Lock()
	defer h.backendsMtx.Unlock()

	for _, name := range backends {
		if backend, ok := h.backends[name]; !ok {
			h.cfg.logger.Warn().
				Str("backend", name).
				Str("url", backend.Cfg.Url.String()).
				Msg("Backend does not exist to remove")
			continue
		} else {
			h.outputChans.sendUnhealthy(backend.toNoti(indefinite()))
			delete(h.backends, backend.Cfg.Name)
		}
	}
}

func (h *healthMonitorImpl) UnhealthyChan() (<-chan *HealthNoti, error) {
	return h.outputChans.unhealthyChannel()
}

func (h *healthMonitorImpl) HealthyChan() (<-chan *HealthNoti, error) {
	return h.outputChans.healthyChannel()
}

func (h *healthMonitorImpl) Size() int {
	h.backendsMtx.RLock()
	defer h.backendsMtx.RUnlock()
	return len(h.backends)
}

func (h *healthMonitorImpl) LastCheckedAt() time.Time {
	return h.lastChecked
}

func (h *healthMonitorImpl) NextCheckAt() time.Time {
	return h.lastChecked.Add(h.cfg.Interval)
}

// healthcheck checks the health of the given backend.
// Make a request to the backend according to the protocol: http, https, tcp, icmp
// If the backend is healthy, returns true.
// Otherwise, returns false
//
// Assumes h.backendsMtx is locked.
func (h *healthMonitorImpl) healthcheck(backend *Backend) (healthy bool, newly bool) {
	var (
		err    error
		logger = h.cfg.logger.With().
			Str("backend", backend.Cfg.Name).
			Logger()
	)

	defer func() {
		if r := recover(); r != nil {
			var ok bool
			if err, ok = r.(error); !ok {
				err = fmt.Errorf("panic: %v", r)
			}
			logger.Err(err).Msg("Panic during health check")

			// Mark this check as a failure
			healthy, newly = backend.fail(h.cfg.UnhealthyThreshold)
		}
		if healthy && newly {
			logger.Info().Msg("Backend entered healthy state")
		} else if !healthy && newly {
			logger.Warn().Msg("Backend entered unhealthy state")
		}
	}()

	switch backend.Cfg.Protocol {
	case HTTP, HTTPS:
		var statusCode int
		statusCode, err = doHttp(h.ctx, backend.Cfg.Url, h.cfg.Timeout)
		if err == nil {
			statusStr := fmt.Sprintf("%d", statusCode)
			ok := false
			for _, pattern := range h.cfg.AcceptStatusCodes {
				if patternMatch(pattern, statusStr) {
					ok = true
					break
				}
			}
			if !ok {
				err = fmt.Errorf("unexpected status code: %d", statusCode)
			}
		}
	case TCP:
		err = doTcp(backend.Cfg.Url, h.cfg.Timeout)
	case ICMP:
		err = doIcmp(backend.Cfg.Url, h.cfg.Timeout)
	}

	// Calculate the fail/success streak
	if err != nil {
		healthy, newly = backend.fail(h.cfg.UnhealthyThreshold)
		logger.Debug().
			AnErr("error", err).
			Int("fail_streak", -backend.statusStreak).
			Msg("Health check failed: did not receive response from backend")
	} else {
		healthy, newly = backend.success(h.cfg.HealthyThreshold)
		logger.Debug().
			Int("success_streak", backend.statusStreak).
			Msg("Health check succeeded: received response from backend")
	}

	return healthy, newly
}

func newOutputChannels(enableHealthyChan bool, enableUnhealthyChan bool) outputChannels {
	o := outputChannels{
		enableHealthyChan:   enableHealthyChan,
		enableUnhealthyChan: enableUnhealthyChan,
	}
	if o.enableHealthyChan {
		o.healthyChan = make(chan *HealthNoti, 1)
	}
	if o.enableUnhealthyChan {
		o.unhealthyChan = make(chan *HealthNoti, 1)
	}
	return o
}

func (o *outputChannels) sendHealthy(noti *HealthNoti) {
	if o.enableHealthyChan {
		o.healthyChan <- noti
	}
}

func (o *outputChannels) sendUnhealthy(noti *HealthNoti) {
	if o.enableUnhealthyChan {
		o.unhealthyChan <- noti
	}
}

func (o *outputChannels) healthyChannel() (<-chan *HealthNoti, error) {
	if o.enableHealthyChan {
		return o.healthyChan, nil
	}
	return nil, ErrChannelNotEnabled
}

func (o *outputChannels) unhealthyChannel() (<-chan *HealthNoti, error) {
	if o.enableUnhealthyChan {
		return o.unhealthyChan, nil
	}
	return nil, ErrChannelNotEnabled
}

func (o *outputChannels) close() {
	if o.enableHealthyChan {
		close(o.healthyChan)
	}
	if o.enableUnhealthyChan {
		close(o.unhealthyChan)
	}
}

func patternMatch(pattern, str string) bool {
	if !strings.HasPrefix(pattern, "^") {
		pattern = "^" + pattern
	}
	if !strings.HasSuffix(pattern, "$") {
		pattern = pattern + "$"
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(str)
}
