package httpclientutil

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	tab := []struct {
		StatusCode int
		RetryAfter string
		Now        time.Time
		Want       time.Duration
	}{
		{StatusCode: http.StatusOK, RetryAfter: "", Want: 0},
		{StatusCode: http.StatusInternalServerError, RetryAfter: "10", Want: 0},
		{StatusCode: http.StatusMovedPermanently, RetryAfter: "10", Want: 10 * time.Second},
		{StatusCode: http.StatusTooManyRequests, RetryAfter: "10", Want: 10 * time.Second},
		{StatusCode: http.StatusServiceUnavailable, RetryAfter: "10", Want: 10 * time.Second},
		{
			StatusCode: http.StatusServiceUnavailable,
			RetryAfter: "Sun, 20 May 2018 07:28:00 GMT",
			Now:        time.Date(2018, 5, 20, 7, 0, 0, 0, time.UTC),
			Want:       28 * time.Minute,
		},

		{StatusCode: http.StatusServiceUnavailable, RetryAfter: "-10", Want: 0},
		{StatusCode: http.StatusServiceUnavailable, RetryAfter: "", Want: 0},
		{StatusCode: http.StatusServiceUnavailable, RetryAfter: "aaa", Want: 0},
	}
	for _, v := range tab {
		var resp http.Response
		resp.StatusCode = v.StatusCode
		resp.Header = make(http.Header)
		if v.RetryAfter != "" {
			resp.Header.Add("Retry-After", v.RetryAfter)
		}
		now := v.Now
		if now.IsZero() {
			now = time.Now()
		}
		if d := ParseRetryAfter(&resp, now); d != v.Want {
			t.Errorf("ParseRetryAfter(Status=%v, RetryAfter=%q) = %v; want %v", v.StatusCode, v.RetryAfter, d, v.Want)
		}
	}
}
