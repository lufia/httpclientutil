package httpclientutil

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lufia/backoff"
	"golang.org/x/time/rate"
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
	NewWaiter func() Waiter
	Transport http.RoundTripper
}

var retriableStatuses = map[int]struct{}{
	http.StatusTooManyRequests:     struct{}{},
	http.StatusInternalServerError: struct{}{},
	http.StatusServiceUnavailable:  struct{}{},
	http.StatusGatewayTimeout:      struct{}{},
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
	ctx := req.Context()
	t := transport(p.Transport)
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

		if d := ParseRetryAfter(resp, time.Now()); d > 0 {
			w.SetNext(d)
		}
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		resp = nil
		if err := w.Wait(ctx); err != nil {
			return nil, err
		}
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
	once *sync.Once
}

func (t *RateLimitTransport) intervalEveryToken() time.Duration {
	return t.Interval / time.Duration(t.Limit)
}

func (t *RateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.once.Do(func() {
		n := rate.Every(t.intervalEveryToken())
		t.l = rate.NewLimiter(n, t.Limit)
	})
	ctx := req.Context()
	err := t.l.Wait(ctx)
	if err != nil {
		return nil, err
	}
	p := transport(t.Transport)
	return p.RoundTrip(req)
}
