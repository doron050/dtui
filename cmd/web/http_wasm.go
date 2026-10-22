//go:build js && wasm

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/dynatrace-oss/dtctl/sdk/session"
)

// installJSTransport replaces the client's HTTP transport with one that hands
// every request to the JS function dtuiFetch. The host runtime there
// performs the authenticated call, so Go never handles tokens, config, or the
// keyring.
func installJSTransport(c *session.Client) {
	c.HTTP().SetTransport(jsRoundTripper{})
}

type jsRoundTripper struct{}

func (jsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		// The RoundTripper contract requires closing the request body, including
		// on error.
		defer req.Body.Close()
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = string(b)
	}

	headers := map[string]any{}
	for k := range req.Header {
		// Auth is supplied by the JS runtime; don't leak the placeholder.
		if k == "Authorization" || k == "Accept-Encoding" {
			continue
		}
		headers[k] = req.Header.Get(k)
	}

	reqObj := map[string]any{
		"method":  req.Method,
		"url":     req.URL.String(),
		"headers": headers,
		"body":    body,
	}

	fetch := js.Global().Get("dtuiFetch")
	if !fetch.Truthy() {
		return nil, fmt.Errorf("dtuiFetch is not defined on the page")
	}
	promise := fetch.Invoke(reqObj)

	// Buffered so the settle callbacks never block, even if RoundTrip has already
	// returned on a canceled context and nobody is receiving.
	resCh := make(chan js.Value, 1)
	errCh := make(chan string, 1)
	// then/catch must outlive an early return: on context cancellation the JS
	// Promise is still pending and will invoke one of them later, and invoking a
	// released js.Func aborts the wasm runtime. A Promise settles exactly once, so
	// releasing both from whichever fires is correct — and it also covers the
	// cancel case, since release then runs when the pending Promise finally
	// settles rather than while it is still in flight.
	var then, catch js.Func
	release := sync.OnceFunc(func() { then.Release(); catch.Release() })
	then = js.FuncOf(func(_ js.Value, args []js.Value) any {
		resCh <- args[0]
		release()
		return nil
	})
	catch = js.FuncOf(func(_ js.Value, args []js.Value) any {
		errCh <- args[0].Call("toString").String()
		release()
		return nil
	})
	promise.Call("then", then).Call("catch", catch)

	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case e := <-errCh:
		return nil, fmt.Errorf("dtuiFetch failed: %s", e)
	case res := <-resCh:
		status := res.Get("status").Int()
		respBody := res.Get("body").String()
		header := http.Header{}
		if h := res.Get("headers"); h.Truthy() {
			keys := js.Global().Get("Object").Call("keys", h)
			for i := 0; i < keys.Length(); i++ {
				k := keys.Index(i).String()
				header.Set(k, h.Get(k).String())
			}
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
			Request:    req,
		}, nil
	}
}
