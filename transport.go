package httpclientutil

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lufia/backoff"
)

type Transporter interface {
	http.RoundTripper
	SetParent(t http.RoundTripper)
}

func Pipeline(c *http.Client, v ...Transporter) {
	t := c.Transport
	for _, p := range v {
		p.SetParent(t)
		t = p
	}
	c.Transport = t
}

// RoundTripperFunc returns
type roundTripperFunc struct {
	t http.RoundTripper
	f func(req *http.Request, t http.RoundTripper) (*http.Response, error)
}

func RoundTripperFunc(f func(req *http.Request, t http.RoundTripper) (*http.Response, error)) Transporter {
	return &roundTripperFunc{
		f: f,
	}
}

func (p *roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	t := p.t
	if t == nil {
		t = http.DefaultTransport
	}
	return p.f(req, t)
}

func (p *roundTripperFunc) SetParent(t http.RoundTripper) {
	p.t = t
}

type Waiter interface {
	Wait(ctx context.Context) error
	SetNext(d time.Duration)
}

// RetriableTransport retries a request that is faile such as 429, 500, 503, or 504.
// And, This retries too when the RoundTripper that is setted to Transport field returns an temporary error.
type RetriableTransport struct {
	NewWaiter func() Waiter
	Transport http.RoundTripper

	// wg counts active requests that is both round-tripping and waiting for retry.
	wg     sync.WaitGroup
	closed int32
}

var retriableStatuses = map[int]struct{}{
	http.StatusTooManyRequests:     struct{}{},
	http.StatusInternalServerError: struct{}{},
	http.StatusServiceUnavailable:  struct{}{},
	http.StatusGatewayTimeout:      struct{}{},
}

func (p *RetriableTransport) transport() http.RoundTripper {
	if p.Transport != nil {
		return p.Transport
	}
	return http.DefaultTransport
}

func (p *RetriableTransport) waiter() Waiter {
	if p.NewWaiter != nil {
		return p.NewWaiter()
	}
	return &backoff.Backoff{}
}

type temporaryer interface {
	Temporary() bool
}

func (p *RetriableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p.wg.Add(1)
	defer p.wg.Done()

	ctx := context.Background()
	t := p.transport()
	w := p.waiter()
	for {
		resp, err := t.RoundTrip(req)
		if err != nil {
			if !isTemporary(err) {
				return nil, err
			}
			w.Wait(ctx)
			continue
		}
		if _, ok := retriableStatuses[resp.StatusCode]; !ok {
			return resp, nil
		}
		// return if resp.StatusCode < 400
		// must read resp.Body
		if d := ParseRetryAfter(resp, time.Now()); d > 0 {
			w.SetNext(d)
		}
		if err := w.Wait(ctx); err != nil {
			return nil, err
		}
	}
}

func isTemporary(err error) bool {
	e, ok := err.(temporaryer)
	return ok && e.Temporary()
}

func (p *RetriableTransport) Close() error {
	return nil
}

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
