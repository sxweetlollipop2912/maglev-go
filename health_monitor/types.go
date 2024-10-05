package health_monitor

import (
	"maglev-go/x/ptr"
	"net/url"
	"time"
)

type Backend struct {
	Url  url.URL
	Name string

	// runtime state
	healthy bool
	// statusStreak is the number of consecutive health checks that have passed or failed.
	// Positive for passing checks, negative for failing checks.
	statusStreak int
}

type HealthNoti struct {
	Url     url.URL
	Name    string
	Healthy bool
	// Timestamp is the time when the health check was performed.
	// If nil, the result will never change again. For example, when the backend is removed.
	Timestamp *time.Time
}

func (b *Backend) toNoti(opts ...func(noti *HealthNoti)) *HealthNoti {
	noti := &HealthNoti{
		Url:       b.Url,
		Name:      b.Name,
		Healthy:   b.healthy,
		Timestamp: ptr.ToPtr(time.Now()),
	}
	for _, opt := range opts {
		opt(noti)
	}
	return noti
}

func indefinite() func(*HealthNoti) {
	return func(noti *HealthNoti) {
		noti.Timestamp = nil
	}
}

func (b *Backend) fail(threshold int) (healthy bool, newly bool) {
	if b.statusStreak > 0 {
		b.statusStreak = 0
	}
	b.statusStreak--
	if b.statusStreak == -threshold {
		b.healthy = false
		newly = true
	}
	return b.healthy, newly
}

func (b *Backend) success(threshold int) (healthy bool, newly bool) {
	if b.statusStreak < 0 {
		b.statusStreak = 0
	}
	b.statusStreak++
	if b.statusStreak == threshold {
		b.healthy = true
		newly = true
	}
	return b.healthy, newly
}
