package utils

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
)

type Status string

const (
	// Status
	StatusReady   Status = "ready"
	StatusRunning Status = "running"
	StatusHealthy Status = "healthy"
	StatusOnline  Status = "online"

	StatusNotReady   Status = "not ready"
	StatusNotRunning Status = "not running"
	StatusNotHealthy Status = "not healthy"
	StatusOffline    Status = "offline"

	// HTTP
	ContentType     = "Content-Type"
	JSONContentType = "application/json"
)

func Sleep(n int) {
	time.Sleep(time.Duration(n) * time.Second)
}

func BoolPtr(b bool) *bool    { return &b }
func Int64Ptr(i int64) *int64 { return &i }

type RetryOptions struct {
	Attempts int
	Delay    time.Duration
}

func Retry(fn func() error, opts RetryOptions) error {
	delay := opts.Delay
	attempts := opts.Attempts

	if attempts < 1 {
		return errors.New("attempts must be >= 1")
	}

	for i := 1; i <= attempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}

		if i == attempts {
			return err
		}

		time.Sleep(delay)
	}

	return nil
}

func Jitter(d time.Duration) time.Duration {
	// ±50% jitter
	j := rand.Float64()*float64(d) - float64(d)/2
	return d + time.Duration(j)
}

func RetryBackoff(fn func() error, opts RetryOptions) error {
	delay := opts.Delay
	attempts := opts.Attempts

	for i := 1; i <= attempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}

		if i == attempts {
			return err
		}

		time.Sleep(Jitter(delay))
		delay *= 2
	}

	return nil
}

// Reversed reverses any given slice
func Reversed[T any](s []T) []T {
	out := make([]T, len(s))
	for i := range s {
		out[len(s)-1-i] = s[i]
	}
	return out
}

// Exit exits in error with a code
func Exit(err error) {
	if err != nil {
		os.Stderr.WriteString(FailureMark() + " " + err.Error() + "\n")
	}
	os.Exit(1)
}

// ValidKubernetesName uses IsDNS1123Subdomain tests for a string that conforms to the
// definition of a subdomain in DNS (RFC 1123).
func ValidKubernetesName(name string) error {
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return fmt.Errorf("invalid name %q: %s", name, strings.Join(errs, "; "))
	}
	return nil
}

// ResolveEnvVar replaces $VAR_NAME or ${VAR_NAME} with its environment variable value.
func ResolveEnvVar(s string) (string, error) {
	if !strings.HasPrefix(s, "$") {
		return s, nil
	}

	val := os.ExpandEnv(s)
	if val == "" {
		return "", fmt.Errorf("env var %q is not set or empty", os.Getenv(s))
	}
	return val, nil
}
