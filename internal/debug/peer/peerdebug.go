package peerdebug

import (
	"context"
	"net"
	"time"
)

type Result struct {
	OK       bool
	Address  string
	Duration string
	Message  string
}

func TestTCP(ctx context.Context, address string, timeout time.Duration) Result {
	start := time.Now()

	// Create a dialer with timeout for testing TCP reachability.
	dialer := net.Dialer{
		Timeout: timeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	duration := time.Since(start).String()

	if err != nil {
		return Result{
			OK:       false,
			Address:  address,
			Duration: duration,
			Message:  err.Error(),
		}
	}

	_ = conn.Close()

	return Result{
		OK:       true,
		Address:  address,
		Duration: duration,
		Message:  "connection successful",
	}
}
