package health_monitor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"time"
)

func doHttp(ctx context.Context, url url.URL, timeout time.Duration) (int, error) {
	client := http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

func doTcp(url url.URL, timeout time.Duration) error {
	address := fmt.Sprintf("%s:%s", url.Hostname(), url.Port())
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func doIcmp(url url.URL, timeout time.Duration) error {
	host := url.Hostname()
	// Execute the 'ping' command
	return exec.Command(
		"ping",
		"-c", "1", "-W", fmt.Sprintf("%.0f", timeout.Seconds()),
		host,
	).Run()
}
