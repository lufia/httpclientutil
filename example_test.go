package httpclientutil_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/lufia/httpclientutil"
)

func Example() {
	f1 := httpclientutil.RoundTripperFunc(func(r *http.Request, t http.RoundTripper) (*http.Response, error) {
		fmt.Println("A")
		return t.RoundTrip(r)
	})
	f2 := httpclientutil.RoundTripperFunc(func(r *http.Request, t http.RoundTripper) (*http.Response, error) {
		fmt.Println("B")
		return t.RoundTrip(r)
	})
	f3 := httpclientutil.RoundTripperFunc(func(r *http.Request, t http.RoundTripper) (*http.Response, error) {
		fmt.Println("C")
		return t.RoundTrip(r)
	})
	var c http.Client
	httpclientutil.Pipeline(&c, f1, f2)
	httpclientutil.Pipeline(&c, f3)

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer s.Close()
	resp, _ := c.Get(s.URL)
	b, _ := ioutil.ReadAll(resp.Body)
	fmt.Println(string(b))
	resp.Body.Close()
	// Output:
	// C
	// B
	// A
	// hello
}
