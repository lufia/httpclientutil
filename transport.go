package httpclientutil

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lufia/backoff"
)

// RoundTripperFunc type is an adapter to allow the use of ordinary functions as
// HTTP round trippers. If f is a function with the appropriate signature,
// RoundTripperFunc(f) is a http.RoundTripper that calls f.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Sender is an interface representing the ability to execute a single
// HTTP transaction.
type Sender interface {
	Send(req *http.Request, next http.RoundTripper) (*http.Response, error)
}

// SenderFunc type is an adapter to allow the use of ordinary functions as
// HTTP round trippers.
type SenderFunc func(req *http.Request, next http.RoundTripper) (*http.Response, error)

// Send executes a single HTTP transaction. t always isn't nil
func (f SenderFunc) Send(req *http.Request, t http.RoundTripper) (*http.Response, error) {
	return f(req, t)
}

// WithTransport returns a new RoundTripper.
// New RoundTripper is set t onto p as a parent RoundTripper.
func WithTransport(p Sender, t http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		next := t
		if next == nil {
			next = http.DefaultTransport
		}
		return p.Send(req, next)
	})
}

func send(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(req)
}

// ContinueWith combines HTTP transaction.
func ContinueWith(c *http.Client, a ...Sender) {
	t := c.Transport
	for _, r := range a {
		t = WithTransport(r, t)
	}
	c.Transport = t
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
