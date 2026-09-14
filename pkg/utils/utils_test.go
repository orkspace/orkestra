package utils

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestBoolPtr(t *testing.T) {
	tr := BoolPtr(true)
	if tr == nil || !*tr {
		t.Error("BoolPtr(true) must return pointer to true")
	}
	fa := BoolPtr(false)
	if fa == nil || *fa {
		t.Error("BoolPtr(false) must return pointer to false")
	}
}

func TestRetry_SuccessFirstAttempt(t *testing.T) {
	calls := 0
	err := Retry(func() error {
		calls++
		return nil
	}, RetryOptions{Attempts: 3, Delay: 0})
	if err != nil || calls != 1 {
		t.Errorf("expected 1 call, got %d; err=%v", calls, err)
	}
}

func TestRetry_SuccessOnRetry(t *testing.T) {
	calls := 0
	err := Retry(func() error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	}, RetryOptions{Attempts: 5, Delay: 0})
	if err != nil {
		t.Errorf("expected nil after retry, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_AllAttemptsFail(t *testing.T) {
	sentinel := errors.New("always fails")
	err := Retry(func() error {
		return sentinel
	}, RetryOptions{Attempts: 3, Delay: 0})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestRetry_ZeroAttempts(t *testing.T) {
	err := Retry(func() error { return nil }, RetryOptions{Attempts: 0, Delay: 0})
	if err == nil {
		t.Error("Retry with 0 attempts must return an error")
	}
}

func TestRetry_OneAttempt_Failure(t *testing.T) {
	sentinel := errors.New("fail")
	err := Retry(func() error { return sentinel }, RetryOptions{Attempts: 1, Delay: 0})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel, got %v", err)
	}
}

func TestRetryBackoff_SuccessOnRetry(t *testing.T) {
	calls := 0
	err := RetryBackoff(func() error {
		calls++
		if calls < 2 {
			return errors.New("not yet")
		}
		return nil
	}, RetryOptions{Attempts: 3, Delay: time.Microsecond})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestRetryBackoff_AllFail(t *testing.T) {
	sentinel := errors.New("always")
	err := RetryBackoff(func() error { return sentinel }, RetryOptions{Attempts: 2, Delay: time.Microsecond})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel, got %v", err)
	}
}

func TestJitter_InRange(t *testing.T) {
	d := 100 * time.Millisecond
	for i := 0; i < 100; i++ {
		j := Jitter(d)
		lo := d / 2   // d - 50% = 50ms
		hi := d + d/2 // d + 50% = 150ms (exclusive upper from rand)
		if j < lo || j > hi {
			t.Errorf("Jitter(%v) = %v, expected in [%v, %v]", d, j, lo, hi)
		}
	}
}

func TestReversed_Empty(t *testing.T) {
	out := Reversed([]int{})
	if len(out) != 0 {
		t.Error("reversed empty slice must be empty")
	}
}

func TestReversed_Single(t *testing.T) {
	out := Reversed([]string{"a"})
	if len(out) != 1 || out[0] != "a" {
		t.Errorf("unexpected: %v", out)
	}
}

func TestReversed_Multiple(t *testing.T) {
	in := []int{1, 2, 3, 4, 5}
	out := Reversed(in)
	want := []int{5, 4, 3, 2, 1}
	for i, v := range want {
		if out[i] != v {
			t.Errorf("index %d: expected %d, got %d", i, v, out[i])
		}
	}
	// Original must be unchanged
	if in[0] != 1 {
		t.Error("Reversed must not modify the original slice")
	}
}

func TestReversed_Strings(t *testing.T) {
	out := Reversed([]string{"a", "b", "c"})
	if out[0] != "c" || out[1] != "b" || out[2] != "a" {
		t.Errorf("unexpected: %v", out)
	}
}

// ── ResolveEnvVar ─────────────────────────────────────────────────────────────

func TestResolveEnvVar_NoPrefix_ReturnAsIs(t *testing.T) {
	val, err := ResolveEnvVar("plain-value")
	if err != nil || val != "plain-value" {
		t.Errorf("no $ prefix must return value unchanged: %q %v", val, err)
	}
}

func TestResolveEnvVar_SetVar_ReturnsValue(t *testing.T) {
	os.Setenv("MERGER_TEST_VAR", "hello-merger")
	defer os.Unsetenv("MERGER_TEST_VAR")

	os.Setenv("MERGER_SECOND_TEST_VAR", "hello-second-merger")
	defer os.Unsetenv("MERGER_SECOND_TEST_VAR")

	val, err := ResolveEnvVar("$MERGER_TEST_VAR")
	if err != nil || val != "hello-merger" {
		t.Errorf("expected hello-merger, got %q err=%v", val, err)
	}

	secondVal, err := ResolveEnvVar("${MERGER_SECOND_TEST_VAR}")
	if err != nil || secondVal != "hello-second-merger" {
		t.Errorf("expected hello-secondmerger, got %q err=%v", val, err)
	}
}

func TestResolveEnvVar_UnsetVar_Error(t *testing.T) {
	os.Unsetenv("MERGER_DEFINITELY_NOT_SET")
	_, err := ResolveEnvVar("$MERGER_DEFINITELY_NOT_SET")
	if err == nil {
		t.Error("unset env var must return error")
	}
}

func TestResolveEnvVar_EmptyVar_Error(t *testing.T) {
	os.Setenv("MERGER_EMPTY_VAR", "")
	defer os.Unsetenv("MERGER_EMPTY_VAR")

	_, err := ResolveEnvVar("$MERGER_EMPTY_VAR")
	if err == nil {
		t.Error("empty env var must return error")
	}
}
