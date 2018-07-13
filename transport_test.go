package httpclientutil

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRetriableTransport(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer s.Close()

	client := &http.Client{
		Transport: &RetriableTransport{},
	}
	resp, err := client.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
}

type tWaiter struct {
	N int
}

func (w *tWaiter) Wait(ctx context.Context) error {
	w.N++
	return nil
}

func (w *tWaiter) SetNext(d time.Duration) {
}

type tError bool

func (err tError) Error() string {
	return "error"
}

func (err tError) Temporary() bool {
	return bool(err)
}

func TestRetriableTransportError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer s.Close()

	const N = 3
	var trialCount int
	f := RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		trialCount++
		if trialCount <= N {
			return nil, tError(true)
		}
		return http.DefaultTransport.RoundTrip(req)
	})
	var w tWaiter
	client := &http.Client{
		Transport: &RetriableTransport{
			NewWaiter: func(r *http.Request) Waiter {
				return &w
			},
			Transport: f,
		},
	}
	resp, err := client.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	if w.N != N {
		t.Errorf("a request waits %d times; want %d", w.N, N)
	}
}

func TestClosableTransport(t *testing.T) {
	var p ClosableTransport
	var c http.Client
	c.Transport = &p

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer s.Close()
	resp, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if s := string(b); s != "hello" {
		t.Errorf("in open: got %v; want %v", s, "hello")
	}

	p.Close()
	_, err = c.Get(s.URL)
	if err == nil {
		t.Errorf("in closed: expects an error")
	}
}

func TestDumpTransport(t *testing.T) {
	var d time.Time
	date := d.UTC().Format(time.RFC1123Z)

	var buf bytes.Buffer
	var c http.Client
	c.Transport = &DumpTransport{
		Output: &buf,
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", date)
		w.Write([]byte("hello"))
	}))
	defer s.Close()

	resp, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	a := strings.Split(buf.String(), "\n")
	for i := range a {
		a[i] = strings.TrimSpace(a[i])
	}

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET / HTTP/1.1",
		"Host: " + u.Host,
		"",
		"HTTP/1.1 200 OK",
		"Content-Length: 5",
		"Content-Type: text/plain; charset=utf-8",
		"Date: Mon, 01 Jan 0001 00:00:00 +0000",
		"",
		"hello",
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("DumpTransport: %v; want %v", a, want)
	}
}
