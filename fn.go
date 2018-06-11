package httpclientutil

import (
	"net/http"
	"strconv"
	"time"
)

func ParseRetryAfter(resp *http.Response, now time.Time) time.Duration {
	switch resp.StatusCode {
	default:
		return 0
	case http.StatusMovedPermanently:
		break
	case http.StatusTooManyRequests:
		break
	case http.StatusServiceUnavailable:
		break
	}
	s := resp.Header.Get("Retry-After")
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	t, err := http.ParseTime(s)
	if err != nil {
		return 0
	}
	return t.Sub(now)
}
