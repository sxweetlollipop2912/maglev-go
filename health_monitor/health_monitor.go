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
	// EnterUnhealthyChan returns a channel that receives newly unhealthy backends.
	EnterUnhealthyChan() (<-chan *HealthNoti, error)
	// EnterHealthyChan returns a channel that receives newly healthy backends.
	EnterHealthyChan() (<-chan *HealthNoti, error)
	// Stop stops the health monitor.
	Stop()
	// IsHealthy returns true if the given backend is healthy.
	IsHealthy(name string) bool
	// Add adds the given backends to the health monitor.
	Add(backends ...*Backend)
	// Remove removes the given backends from the health monitor.
	Remove(backends ...*Backend)
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
	h.outputChans.close()
	<-h.tickerStopped
}

func (h *healthMonitorImpl) IsHealthy(name string) bool {
	h.backendsMtx.RLock()
	defer h.backendsMtx.RUnlock()

	if backend, ok := h.backends[name]; ok {
		return backend.healthy
	}
	return false
}

func (h *healthMonitorImpl) Add(backends ...*Backend) {
	h.backendsMtx.Lock()
	defer h.backendsMtx.Unlock()

	for i := range backends {
		backend := backends[i]
		if backend, ok := h.backends[backend.Name]; ok {
			h.cfg.logger.Warn().
				Str("backend", backend.Name).
				Str("url", backend.Url.String()).
				Bool("healthy", backend.healthy).
				Msg("Backend already exists")
			continue
		}
		backend.healthy = h.cfg.HealthyInitially
		h.backends[backend.Name] = backend

		if backend.healthy {
			h.outputChans.sendHealthy(backend.toNoti())
		} else {
			h.outputChans.sendUnhealthy(backend.toNoti())
		}
	}
}

func (h *healthMonitorImpl) Remove(backends ...*Backend) {
	h.backendsMtx.Lock()
	defer h.backendsMtx.Unlock()

	for _, backend := range backends {
		if _, ok := h.backends[backend.Name]; !ok {
			h.cfg.logger.Warn().
				Str("backend", backend.Name).
				Str("url", backend.Url.String()).
				Msg("Backend does not exist to remove")
			continue
		}
		h.outputChans.sendUnhealthy(backend.toNoti(indefinite()))
		delete(h.backends, backend.Name)
	}
}

func (h *healthMonitorImpl) EnterUnhealthyChan() (<-chan *HealthNoti, error) {
	return h.outputChans.unhealthyChannel()
}

func (h *healthMonitorImpl) EnterHealthyChan() (<-chan *HealthNoti, error) {
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
			Str("backend", backend.Name).
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

	switch h.cfg.Protocol {
	case HTTP, HTTPS:
		var statusCode int
		statusCode, err = doHttp(h.ctx, backend.Url, h.cfg.HttpPath, h.cfg.Timeout)
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
		err = doTcp(backend.Url, h.cfg.Timeout)
	case ICMP:
		err = doIcmp(backend.Url, h.cfg.Timeout)
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