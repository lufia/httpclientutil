package httpclientutil

import (
	"io"
	"net/http"
	"net/http/httputil"
	"os"
)

type DumpTransport struct {
	Output        io.Writer
	IncludingBody bool
}

func (d *DumpTransport) Send(req *http.Request, t http.RoundTripper) (*http.Response, error) {
	w := d.Output
	if w == nil {
		w = os.Stdout
	}

	b, err := httputil.DumpRequest(req, d.IncludingBody)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	resp, err := t.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	b, err = httputil.DumpResponse(resp, d.IncludingBody)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	return resp, nil
}
