package rate_limiter

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestInMemoryRateLimiter(t *testing.T) {
	clock := &mockClock{time: time.Now()}
	rl := NewInMemoryRateLimiter(clock, RateLimitingPolicy{Limit: 5, Window: 30 * time.Minute})

	for i := 1; i <= 5; i++ {
		allow, timeRemaining, err := rl.Allow("ip:127.0.0.1")
		if err != nil || !allow {
			t.Fatalf("request %v should not be rate limited: %v (err: %v)", i, timeRemaining, err)
		}
		clock.IncTime(time.Minute)
	}

	// 6th request should be rate limited
	allow, timeRemaining, _ := rl.Allow("ip:127.0.0.1")
	if allow {
		t.Fatalf("expected sixth request to be limited")
	}
	if timeRemaining.Round(time.Minute) != 25*time.Minute {
		t.Fatalf("remaining time should be about 25 minutes, but was %v", timeRemaining)
	}

	// a different key has its own budget
	if allow, _, _ := rl.Allow("ip:127.0.0.2"); !allow {
		t.Fatalf("other key should not be limited")
	}

	clock.IncTime(30 * time.Minute)
	if allow, timeRemaining, _ = rl.Allow("ip:127.0.0.1"); !allow {
		t.Fatalf("expected timeout to be over: time remaining %v", timeRemaining)
	}
}

func TestRedisRateLimiterAllowsUpToLimit(t *testing.T) {
	rl, _ := newTestRedisRateLimiter(t, RateLimitingPolicy{Limit: 5, Window: 30 * time.Minute})

	// a limit of 5 should allow exactly 5 requests
	for i := 1; i <= 5; i++ {
		allow, _, err := rl.Allow("ip:127.0.0.1")
		if err != nil {
			t.Fatalf("request %v returned unexpected error: %v", i, err)
		}
		if !allow {
			t.Fatalf("request %v should be allowed", i)
		}
	}

	// the 6th request should be blocked
	allow, timeout, err := rl.Allow("ip:127.0.0.1")
	if err != nil {
		t.Fatalf("blocked request returned unexpected error: %v", err)
	}
	if allow {
		t.Fatalf("6th request should be blocked")
	}
	if timeout.Round(time.Minute) != 30*time.Minute {
		t.Fatalf("retry-after should be about 30 minutes, but was %v", timeout)
	}
}

func TestRedisRateLimiterRetryAfterShrinksWithTime(t *testing.T) {
	rl, mr := newTestRedisRateLimiter(t, RateLimitingPolicy{Limit: 2, Window: 10 * time.Minute})

	for i := 1; i <= 2; i++ {
		allow, _, err := rl.Allow("ip:127.0.0.1")
		if err != nil || !allow {
			t.Fatalf("request %v should be allowed (err: %v)", i, err)
		}
	}

	mr.FastForward(4 * time.Minute)

	allow, timeout, err := rl.Allow("ip:127.0.0.1")
	if err != nil {
		t.Fatalf("blocked request returned unexpected error: %v", err)
	}
	if allow {
		t.Fatalf("3rd request should be blocked")
	}
	if timeout.Round(time.Minute) != 6*time.Minute {
		t.Fatalf("retry-after should be about 6 minutes, but was %v", timeout)
	}
}

func TestRedisRateLimiterWindowExpiryResetsCount(t *testing.T) {
	rl, mr := newTestRedisRateLimiter(t, RateLimitingPolicy{Limit: 2, Window: 10 * time.Minute})

	for i := 1; i <= 2; i++ {
		if allow, _, err := rl.Allow("ip:127.0.0.1"); err != nil || !allow {
			t.Fatalf("request %v should be allowed (err: %v)", i, err)
		}
	}
	if allow, _, _ := rl.Allow("ip:127.0.0.1"); allow {
		t.Fatalf("3rd request should be blocked")
	}

	// after the window passes the counter expires and requests are allowed again
	mr.FastForward(10 * time.Minute)

	allow, _, err := rl.Allow("ip:127.0.0.1")
	if err != nil {
		t.Fatalf("request after window expiry returned unexpected error: %v", err)
	}
	if !allow {
		t.Fatalf("request after window expiry should be allowed")
	}
}

func TestRedisRateLimiterKeysAreIndependent(t *testing.T) {
	rl, _ := newTestRedisRateLimiter(t, RateLimitingPolicy{Limit: 1, Window: 10 * time.Minute})

	if allow, _, err := rl.Allow("phone:+31611111111"); err != nil || !allow {
		t.Fatalf("first request for first phone should be allowed (err: %v)", err)
	}
	if allow, _, _ := rl.Allow("phone:+31611111111"); allow {
		t.Fatalf("second request for first phone should be blocked")
	}

	// a different key has its own counter
	if allow, _, err := rl.Allow("phone:+31622222222"); err != nil || !allow {
		t.Fatalf("first request for second phone should be allowed (err: %v)", err)
	}
}

func TestRedisRateLimiterNamespacesAreIndependent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	policy := RateLimitingPolicy{Limit: 1, Window: 10 * time.Minute}
	sendSms := NewRedisRateLimiter(client, "send-sms", policy)
	verifyCode := NewRedisRateLimiter(client, "verify-code", policy)

	if allow, _, err := sendSms.Allow("ip:127.0.0.1"); err != nil || !allow {
		t.Fatalf("first send-sms request should be allowed (err: %v)", err)
	}
	if allow, _, _ := sendSms.Allow("ip:127.0.0.1"); allow {
		t.Fatalf("second send-sms request should be blocked")
	}

	// the same key in another namespace has its own counter
	if allow, _, err := verifyCode.Allow("ip:127.0.0.1"); err != nil || !allow {
		t.Fatalf("verify-code request should not share the send-sms counter (err: %v)", err)
	}
}

func TestRedisRateLimiterRedisFailure(t *testing.T) {
	rl, mr := newTestRedisRateLimiter(t, RateLimitingPolicy{Limit: 5, Window: 10 * time.Minute})

	mr.SetError("connection lost")

	allow, _, err := rl.Allow("ip:127.0.0.1")
	if err == nil {
		t.Fatalf("expected an error when redis fails")
	}
	if allow {
		t.Fatalf("request should not be allowed when redis fails")
	}
}

func newTestRedisRateLimiter(t *testing.T, policy RateLimitingPolicy) (*RedisRateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisRateLimiter(client, "test", policy), mr
}

type mockClock struct {
	time time.Time
}

func (c *mockClock) GetTime() time.Time {
	return c.time
}

func (c *mockClock) IncTime(time time.Duration) {
	c.time = c.time.Add(time)
}
