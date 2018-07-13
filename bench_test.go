package httpclientutil

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lufia/backoff"
)

type tCount struct {
	n    int
	peak int
	mu   sync.RWMutex
}

func (c *tCount) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	if c.n > c.peak {
		c.peak = c.n
	}
}

func (c *tCount) Dec() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n--
}

func (c *tCount) Peak() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.peak
}

type tCountTransport struct {
	Transport http.RoundTripper
	Count     tCount
}

func (t *tCountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.Count.Inc()
	defer t.Count.Dec()
	if t.Transport != nil {
		return t.Transport.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func BenchmarkRetriableTransport(b *testing.B) {
	var count tCount
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Inc()
		defer count.Dec()
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte("hello"))
	}))
	defer s.Close()

	client := &http.Client{
		Transport: &tCountTransport{
			Transport: &RetriableTransport{
				NewWaiter: func(r *http.Request) Waiter {
					return &backoff.Backoff{Limit: 3, Initial: 2}
				},
				Transport: &http.Transport{
					MaxIdleConnsPerHost: 1,
				},
			},
		},
	}
	b.Logf("N = %d", b.N)
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(s.URL)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(ioutil.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
	t := client.Transport.(*tCountTransport)
	b.Logf("ServerPeak = %d", count.Peak())
	b.Logf("ClientPeak = %d", t.Count.Peak())
}
