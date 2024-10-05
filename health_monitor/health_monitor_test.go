package health_monitor

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestHealthMonitorDifferentScenarios tests health monitor for different scenarios, using HTTP protocol
func TestHealthMonitorDifferentScenarios(t *testing.T) {
	tests := []struct {
		name            string
		setupContainers func(t *testing.T, monitor HealthMonitor) ([]*Backend, func())
		expectedStates  map[string][]bool // A list of expected states before and after transitions
		checkCnt        int
		protocol        Protocol
	}{
		{
			name: "health check for a healthy HTTP backend",
			setupContainers: func(t *testing.T, _ HealthMonitor) ([]*Backend, func()) {
				_, backend, teardown := setupHTTPBackend(t, "http-backend", 8080, false) // HTTP with 200 OK
				return []*Backend{backend}, teardown
			},
			expectedStates: map[string][]bool{
				"http-backend": {true}, // Initial state
			},
			checkCnt: 1,
			protocol: HTTP,
		},
		{
			name: "health check for an unhealthy HTTP backend",
			setupContainers: func(t *testing.T, _ HealthMonitor) ([]*Backend, func()) {
				_, backend, teardown := setupHTTPBackend(t, "http-backend", 8081, true)
				return []*Backend{backend}, teardown
			},
			expectedStates: map[string][]bool{
				"http-backend": {false}, // Initial state
			},
			checkCnt: 1,
			protocol: HTTP,
		},
		{
			name: "health check for a healthy, but then offline HTTP backend",
			setupContainers: func(t *testing.T, _ HealthMonitor) ([]*Backend, func()) {
				_, backend, teardown := setupHTTPBackend(t, "http-backend", 8082, false) // Start healthy
				go func() {
					time.Sleep(3 * time.Second)
					teardown() // Turn backend offline after 3 seconds
				}()
				return []*Backend{backend}, func() {}
			},
			expectedStates: map[string][]bool{
				"http-backend": {true, false}, // Initially healthy
			},
			checkCnt: 2,
			protocol: HTTP,
		},
		{
			name: "health check for an offline, but then healthy HTTP backend",
			setupContainers: func(t *testing.T, hm HealthMonitor) ([]*Backend, func()) {
				go func() {
					_, backend, teardown := setupHTTPBackend(t, "http-backend", 8083, true)
					hm.Add(backend)

					time.Sleep(4 * time.Second)
					teardown()
					_, backend, teardown = setupHTTPBackend(t, "http-backend", 8083, false)
					time.Sleep(7 * time.Second)
					teardown()
				}()
				return []*Backend{}, func() {}
			},
			expectedStates: map[string][]bool{
				"http-backend": {false, true}, // Initially offline
			},
			checkCnt: 2,
			protocol: HTTP,
		},
		{
			name: "health check for multiple backends, some healthy, some unhealthy",
			setupContainers: func(t *testing.T, _ HealthMonitor) ([]*Backend, func()) {
				_, healthyBackend, healthyTeardown := setupHTTPBackend(t, "http-backend-healthy", 8085, false)
				_, unhealthyBackend, unhealthyTeardown := setupHTTPBackend(t, "http-backend-unhealthy", 8086, true)
				return []*Backend{healthyBackend, unhealthyBackend}, func() {
					healthyTeardown()
					unhealthyTeardown()
				}
			},
			expectedStates: map[string][]bool{
				"http-backend-healthy":   {true},
				"http-backend-unhealthy": {false},
			},
			checkCnt: 1,
			protocol: HTTP,
		},
		{
			name: "health check but add and remove backends while running",
			setupContainers: func(t *testing.T, hm HealthMonitor) ([]*Backend, func()) {
				_, backend, teardown := setupHTTPBackend(t, "http-backend", 8087, false) // Initially healthy
				go func() {
					time.Sleep(5 * time.Second)
					_, backendToAdd, addTeardown := setupHTTPBackend(t, "http-backend-2", 8088, false) // Add healthy backend
					hm.Add(backendToAdd)
					defer addTeardown()
					time.Sleep(7 * time.Second)
					hm.Remove(backend)
					time.Sleep(5 * time.Second)
				}()
				return []*Backend{backend}, teardown
			},
			expectedStates: map[string][]bool{
				"http-backend":   {true, true, false},
				"http-backend-2": {false, true, true},
			},
			checkCnt: 3,
			protocol: HTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			hm, err := NewHealthMonitor(ctx,
				WithProtocol(tt.protocol),
				WithCheckInterval(1*time.Second),
			)
			assert.NoError(t, err)

			// Setup the containers
			backends, teardown := tt.setupContainers(t, hm)
			defer teardown()

			// Add backends to the health monitor
			hm.Add(backends...)

			// Start the health monitor
			err = hm.Start()
			assert.NoError(t, err)

			// Verify the health states during transitions
			for i := 0; i < tt.checkCnt; i++ {
				// Wait before checking the next state transition
				time.Sleep(5 * time.Second)
				for backendName, expectedHealthy := range tt.expectedStates {
					assert.Equal(t, expectedHealthy[i], hm.IsHealthy(backendName), fmt.Sprintf("Expected backend %s to be healthy: %v", backendName, expectedHealthy[i]))
				}
			}

			// Stop the health monitor
			hm.Stop()
		})
	}
}

// TestHealthMonitorChannels tests health check state changes via health/unhealthy channels
func TestHealthMonitorChannels(t *testing.T) {
	ctx := context.Background()
	hm, err := NewHealthMonitor(ctx,
		WithProtocol(HTTP),
		EnableHealthyChannel(),
		EnableUnhealthyChannel(),
		WithCheckInterval(1*time.Second),
	)
	assert.NoError(t, err)

	// Setup HTTP backend
	_, backend, teardown := setupHTTPBackend(t, "http-backend", 8089, true) // Initially unhealthy
	defer teardown()

	// Add backend to the health monitor
	hm.Add(backend)

	// Start the health monitor
	err = hm.Start()
	assert.NoError(t, err)

	unhealthyChan, err := hm.EnterUnhealthyChan()
	assert.NoError(t, err)
	healthyChan, err := hm.EnterHealthyChan()
	assert.NoError(t, err)

	// Wait for the backend to become unhealthy
	select {
	case unhealthy := <-unhealthyChan:
		assert.Equal(t, "http-backend", unhealthy.Name)
		assert.False(t, unhealthy.Healthy)
	case <-time.After(5 * time.Second):
		t.Fatal("Expected to receive unhealthy notification")
	}

	// Simulate backend becoming healthy by changing the response code
	teardown()
	_, backendHealthy, teardown := setupHTTPBackend(t, "http-backend", 8090, false) // Now healthy
	defer teardown()

	hm.Add(backendHealthy)

	// Wait for the backend to become healthy
	select {
	case healthy := <-healthyChan:
		assert.Equal(t, "http-backend", healthy.Name)
		assert.True(t, healthy.Healthy)
	case <-time.After(5 * time.Second):
		t.Fatal("Expected to receive healthy notification")
	}

	hm.Stop()
}

func TestHealthMonitorDifferentProtocols(t *testing.T) {
	tests := []struct {
		name            string
		setupContainers func(t *testing.T) ([]*Backend, func())
		expectedStates  map[string]bool
		protocol        Protocol
	}{
		{
			name: "health check for a healthy HTTP backend",
			setupContainers: func(t *testing.T) ([]*Backend, func()) {
				_, backend, teardown := setupHTTPBackend(t, "http-backend", 8091, false) // HTTP with 200 OK
				return []*Backend{backend}, teardown
			},
			expectedStates: map[string]bool{"http-backend": true},
			protocol:       HTTP,
		},
		{
			name: "health check for an unhealthy HTTP backend",
			setupContainers: func(t *testing.T) ([]*Backend, func()) {
				_, backend, teardown := setupHTTPBackend(t, "http-backend", 8092, true) // HTTP with 500 Internal Server Error
				return []*Backend{backend}, teardown
			},
			expectedStates: map[string]bool{"http-backend": false},
			protocol:       HTTP,
		},
		{
			name: "health check for a healthy TCP backend",
			setupContainers: func(t *testing.T) ([]*Backend, func()) {
				_, backend, teardown := setupTCPBackend(t) // TCP server
				return []*Backend{backend}, teardown
			},
			expectedStates: map[string]bool{"tcp-backend": true},
			protocol:       TCP,
		},
		{
			name: "health check for a healthy ICMP backend",
			setupContainers: func(t *testing.T) ([]*Backend, func()) {
				backend := createICMPBackend(t, "8.8.8.8") // Google public DNS as ICMP
				return []*Backend{backend}, func() {}      // No teardown needed for real services
			},
			expectedStates: map[string]bool{"icmp-backend": true},
			protocol:       ICMP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			hm, err := NewHealthMonitor(ctx, WithProtocol(tt.protocol), WithCheckInterval(1*time.Second))
			assert.NoError(t, err)

			// Setup the containers
			backends, teardown := tt.setupContainers(t)
			defer teardown()

			// Add backends to the health monitor
			hm.Add(backends...)

			// Start the health monitor
			err = hm.Start()
			assert.NoError(t, err)

			// Wait for the health monitor to finish the initial health checks
			time.Sleep(5 * time.Second)

			// Verify the health state of each backend
			for backendName, expectedHealthy := range tt.expectedStates {
				assert.Equal(t, expectedHealthy, hm.IsHealthy(backendName), fmt.Sprintf("Expected backend %s to be healthy: %v", backendName, expectedHealthy))
			}

			// Stop the health monitor
			hm.Stop()
		})
	}
}

// Helper function to create an HTTP backend using testcontainers-go
func setupHTTPBackend(t *testing.T, name string, port int, dontSetup bool) (testcontainers.Container, *Backend, func()) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "nginx", // Use NGINX as a simple HTTP server
		ExposedPorts: []string{fmt.Sprintf("%d:80/tcp", port)},
		WaitingFor:   wait.ForHTTP("/").WithStatusCodeMatcher(func(status int) bool { return status == 200 }),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	assert.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "80/tcp")
	assert.NoError(t, err)
	assert.Equal(t, port, mappedPort.Int())

	host, err := container.Host(ctx)
	assert.NoError(t, err)
	assert.NoError(t, err)

	// Create a Backend object pointing to the test container
	backendURL, _ := url.Parse(fmt.Sprintf("http://%s:%d", host, port))
	backend := &Backend{
		Url:  *backendURL,
		Name: name,
	}

	teardown := func() {
		_ = container.Terminate(ctx)
	}

	if dontSetup {
		teardown()
		teardown = func() {}
	}

	return container, backend, teardown
}

// Helper function to create a TCP backend using testcontainers-go
func setupTCPBackend(t *testing.T) (testcontainers.Container, *Backend, func()) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "redis", // Redis exposes TCP on port 6379
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	assert.NoError(t, err)

	host, err := container.Host(ctx)
	assert.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379/tcp")
	assert.NoError(t, err)

	// Create a Backend object pointing to the test container
	backendURL, _ := url.Parse(fmt.Sprintf("tcp://%s:%s", host, port.Port()))
	backend := &Backend{
		Url:  *backendURL,
		Name: "tcp-backend",
	}

	teardown := func() {
		_ = container.Terminate(ctx)
	}

	return container, backend, teardown
}

// Helper function to create an ICMP backend (Google public DNS for example)
func createICMPBackend(_ *testing.T, address string) *Backend {
	backendURL, _ := url.Parse(fmt.Sprintf("icmp://%s", address))
	return &Backend{
		Url:  *backendURL,
		Name: "icmp-backend",
	}
}
