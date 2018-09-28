package httpclientutil

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/lufia/backoff"
)

func transport(t http.RoundTripper) http.RoundTripper {
	if t != nil {
		return t
	}
	return http.DefaultTransport
}

type Waiter interface {
	Wait(ctx context.Context) error
	SetNext(d time.Duration)
}

// RetriableTransport retries a request that is faile such as 429, 500, 503, or 504.
// And, This retries too when the RoundTripper that is setted to Transport field returns an temporary error that implement Temporary() bool.
type RetriableTransport struct {
	// Peak is maximum duration. Zero is no limit.
	Peak time.Duration
	// Initial is initial duration.
	Initial time.Duration
	// Limit is maximum retry count.
	Limit int
	// MaxAge is maximum time until transport is force expired.
	MaxAge time.Duration

	// NewWaiter overrides above parameters.
	NewWaiter func(r *http.Request) Waiter
	Transport http.RoundTripper
}

var retriableStatuses = map[int]struct{}{
	http.StatusTooManyRequests:     struct{}{},
	http.StatusInternalServerError: struct{}{},
	http.StatusServiceUnavailable:  struct{}{},
	http.StatusGatewayTimeout:      struct{}{},
}

func (p *RetriableTransport) waiter(r *http.Request) Waiter {
	if p.NewWaiter != nil {
		return p.NewWaiter(r)
	}
	return &backoff.Backoff{
		Peak:    p.Peak,
		Initial: p.Initial,
		Limit:   p.Limit,
		MaxAge:  p.MaxAge,
	}
}

type temporaryer interface {
	Temporary() bool
}

const countKey = "X-Retry-Count"

func RetryCount(resp *http.Response) int {
	s := resp.Header.Get(countKey)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// RoundTrip implements the http.RoundTripper interface.
// This returns a response with X-Retry-Count header.
func (p *RetriableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var retryCount int

	ctx := req.Context()
	t := transport(p.Transport)
	w := p.waiter(req)
	for {
		resp, err := t.RoundTrip(req)
		if err != nil {
			if !isTemporary(err) {
				// TODO(lufia): should set retry-count header
				return nil, err
			}
			w.Wait(ctx)
			retryCount++
			continue
		}
		resp.Header.Set(countKey, strconv.Itoa(retryCount))
		if _, ok := retriableStatuses[resp.StatusCode]; !ok {
			return resp, nil
		}

		if d := ParseRetryAfter(resp, time.Now()); d > 0 {
			w.SetNext(d)
		}
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		resp = nil
		if err := w.Wait(ctx); err != nil {
			return nil, err
		}
		retryCount++
	}
}

func isTemporary(err error) bool {
	e, ok := err.(temporaryer)
	return ok && e.Temporary()
}

type ClosableTransport struct {
	Transport http.RoundTripper

	// wg counts active requests that is both round tripping and waiting for retry.
	wg     sync.WaitGroup
	closed int32
}

func (p *ClosableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if v := atomic.LoadInt32(&p.closed); v != 0 {
		return nil, errors.New("round tripper is closed")
	}
	p.wg.Add(1)
	defer p.wg.Done()

	t := transport(p.Transport)
	return t.RoundTrip(req)
}

// Close closes transport and waits all requests is done.
func (p *ClosableTransport) Close() error {
	atomic.StoreInt32(&p.closed, 1)
	p.wg.Wait()
	return nil
}

type DumpTransport struct {
	Transport   http.RoundTripper
	Output      io.Writer
	WithoutBody bool
}

func (t *DumpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := t.Output
	if w == nil {
		w = os.Stdout
	}

	b, err := httputil.DumpRequest(req, !t.WithoutBody)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	p := transport(t.Transport)
	resp, err := p.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	b, err = httputil.DumpResponse(resp, !t.WithoutBody)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	return resp, nil
}

// RateLimitTransport limits requests through transport.
// This will blocks requests over limit until decrease of rate.
type RateLimitTransport struct {
	Transport http.RoundTripper
	Interval  time.Duration
	Limit     int

	l    *rate.Limiter
	once sync.Once
}

func (t *RateLimitTransport) interval() time.Duration {
	return t.Interval / time.Duration(t.Limit)
}

func (t *RateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.once.Do(func() {
		n := rate.Every(t.interval())
		t.l = rate.NewLimiter(n, t.Limit)
	})
	ctx := req.Context()
	if err := t.l.Wait(ctx); err != nil {
		return nil, err
	}
	p := transport(t.Transport)
	return p.RoundTrip(req)
}

// SemaphoreTransport restricts number of concurrent requests up to Limit.
//
// Go 1.11 or later, you could use http.Client.MaxConnsPerHost.
type SemaphoreTransport struct {
	Transport http.RoundTripper
	Limit     int

	w    *semaphore.Weighted
	once sync.Once
}

func (t *SemaphoreTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.once.Do(func() {
		t.w = semaphore.NewWeighted(int64(t.Limit))
	})
	ctx := req.Context()
	if err := t.w.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer t.w.Release(1)
	p := transport(t.Transport)
	return p.RoundTrip(req)
}
