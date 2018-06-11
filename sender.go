package httpclientutil

import (
	"net/http"
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
