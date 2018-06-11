package httpclientutil_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/lufia/httpclientutil"
)

func ExampleContinueWith() {
	f1 := httpclientutil.SenderFunc(func(r *http.Request, t http.RoundTripper) (*http.Response, error) {
		fmt.Println("A1")
		defer fmt.Println("A2")
		return t.RoundTrip(r)
	})
	f2 := httpclientutil.SenderFunc(func(r *http.Request, t http.RoundTripper) (*http.Response, error) {
		fmt.Println("B1")
		defer fmt.Println("B2")
		return t.RoundTrip(r)
	})
	f3 := httpclientutil.SenderFunc(func(r *http.Request, t http.RoundTripper) (*http.Response, error) {
		fmt.Println("C1")
		defer fmt.Println("C2")
		return t.RoundTrip(r)
	})
	var c http.Client
	c.Transport = httpclientutil.ContinueWith(c.Transport, f1, f2)
	c.Transport = httpclientutil.ContinueWith(c.Transport, f3)

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer s.Close()
	resp, _ := c.Get(s.URL)
	b, _ := ioutil.ReadAll(resp.Body)
	fmt.Println(string(b))
	resp.Body.Close()
	// Output:
	// C1
	// B1
	// A1
	// A2
	// B2
	// C2
	// hello
}
