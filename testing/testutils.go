package testutils

import (
	"os"
	"time"
)

func ScaleDuration(d time.Duration) time.Duration {
	if os.Getenv("CI") != "" {
		d *= 10
	}
	return d
}
