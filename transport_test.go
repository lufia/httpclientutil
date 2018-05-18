package httpclientutil

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lufia/backoff"
)

func TestRetriableTransport(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer s.Close()

	client := &http.Client{
		Transport: &RetriableTransport{
			NewWaiter: func() Waiter {
				return &backoff.Backoff{Limit: 1, Initial: 2}
			},
			Transport: &http.Transport{
				MaxIdleConns: 1,
			},
		},
	}
	resp, err := client.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
}

func TestRetriableTransportError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach here")
	}))
	defer s.Close()

	f := RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("error")
	})
	client := &http.Client{
		Transport: &RetriableTransport{
			Transport: &f,
		},
	}
	resp, err := client.Get(s.URL)
	if err == nil {
		t.Errorf("Get(%q) expects an error", s.URL)
	}
	if resp != nil {
		t.Fatal("response must be nil")
	}
}

func TestParseRetryAfter(t *testing.T) {
	tab := []struct {
		StatusCode int
		RetryAfter string
		Want       time.Duration
	}{
		{StatusCode: http.StatusOK, RetryAfter: "", Want: 0},
		{StatusCode: http.StatusInternalServerError, RetryAfter: "10", Want: 0},
		{StatusCode: http.StatusMovedPermanently, RetryAfter: "10", Want: 10 * time.Second},
		{StatusCode: http.StatusTooManyRequests, RetryAfter: "10", Want: 10 * time.Second},
		{StatusCode: http.StatusServiceUnavailable, RetryAfter: "10", Want: 10 * time.Second},
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
		if d := ParseRetryAfter(&resp); d != v.Want {
			t.Errorf("ParseRetryAfter(Status=%v, RetryAfter=%q) = %v; want %v", v.StatusCode, v.RetryAfter, d, v.Want)
		}
	}
}
