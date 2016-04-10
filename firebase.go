/*
Package firego is a REST client for Firebase (https://firebase.com).
*/
package firego

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	_url "net/url"
	"strings"
	"sync"
	"time"
)

// TimeoutDuration is the length of time any request will have to establish
// a connection and receive headers from Firebase before returning
// an ErrTimeout error.
var TimeoutDuration = 30 * time.Second

var defaultRedirectLimit = 30

// ErrTimeout is an error type is that is returned if a request
// exceeds the TimeoutDuration configured.
type ErrTimeout struct {
	error
}

// query parameter constants
const (
	authParam         = "auth"
	formatParam       = "format"
	shallowParam      = "shallow"
	orderByParam      = "orderBy"
	startAtParam      = "startAt"
	endAtParam        = "endAt"
	formatVal         = "export"
	limitToFirstParam = "limitToFirst"
	limitToLastParam  = "limitToLast"
)

// Firebase represents a location in the cloud.
type Firebase struct {
	url    string
	params _url.Values
	client *http.Client

	eventMtx   sync.Mutex
	eventFuncs map[string]chan struct{}

	watchMtx     sync.Mutex
	watching     bool
	stopWatching chan struct{}
}

func sanitizeURL(url string) string {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		url = "https://" + url
	}

	if strings.HasSuffix(url, "/") {
		url = url[:len(url)-1]
	}

	return url
}

func sanitizePath(p string) string {
	// remove slashes from the front and back
	//	/foo/.json -> foo/.json
	s := strings.Trim(p, "/")

	// remove .json extension
	//	foo/.json -> foo/
	s = strings.TrimSuffix(s, ".json")

	// trim an potential trailing slashes
	//	foo/ -> foo
	return strings.TrimSuffix(s, "/")
}

// Preserve headers on redirect.
//
// Reference https://github.com/golang/go/issues/4800
func redirectPreserveHeaders(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		// No redirects
		return nil
	}

	if len(via) > defaultRedirectLimit {
		return fmt.Errorf("%d consecutive requests(redirects)", len(via))
	}

	// mutate the subsequent redirect requests with the first Header
	for key, val := range via[0].Header {
		req.Header[key] = val
	}
	return nil
}

// New creates a new Firebase reference,
// if client is nil, http.DefaultClient is used.
func New(url string, client *http.Client) *Firebase {

	if client == nil {
		var tr *http.Transport
		tr = &http.Transport{
			DisableKeepAlives: true, // https://code.google.com/p/go/issues/detail?id=3514
			Dial: func(network, address string) (net.Conn, error) {
				start := time.Now()
				c, err := net.DialTimeout(network, address, TimeoutDuration)
				tr.ResponseHeaderTimeout = TimeoutDuration - time.Since(start)
				return c, err
			},
		}

		client = &http.Client{
			Transport:     tr,
			CheckRedirect: redirectPreserveHeaders,
		}
	}

	return &Firebase{
		url:          sanitizeURL(url),
		params:       _url.Values{},
		client:       client,
		stopWatching: make(chan struct{}),
		eventFuncs:   map[string]chan struct{}{},
	}
}

// String returns the string representation of the
// Firebase reference.
func (fb *Firebase) String() string {
	path := fb.url + "/.json"

	if len(fb.params) > 0 {
		path += "?" + fb.params.Encode()
	}
	return path
}

// Child creates a new Firebase reference for the requested
// child with the same configuration as the parent.
func (fb *Firebase) Child(child string) *Firebase {
	c := fb.copy()
	c.url = c.url + "/" + child
	return c
}

func (fb *Firebase) copy() *Firebase {
	c := &Firebase{
		url:          fb.url,
		params:       _url.Values{},
		client:       fb.client,
		stopWatching: make(chan struct{}),
		eventFuncs:   map[string]chan struct{}{},
	}

	// making sure to manually copy the map items into a new
	// map to avoid modifying the map reference.
	for k, v := range fb.params {
		c.params[k] = v
	}
	return c
}

func (fb *Firebase) makeRequest(method string, body []byte) (*http.Request, error) {
	return http.NewRequest(method, fb.String(), bytes.NewReader(body))
}

func (fb *Firebase) doRequest(method string, body []byte) ([]byte, error) {
	req, err := fb.makeRequest(method, body)
	if err != nil {
		return nil, err
	}

	resp, err := fb.client.Do(req)
	switch err := err.(type) {
	default:
		return nil, err
	case nil:
		// carry on

	case *_url.Error:
		// `http.Client.Do` will return a `url.Error` that wraps a `net.Error`
		// when exceeding it's `Transport`'s `ResponseHeadersTimeout`
		e1, ok := err.Err.(net.Error)
		if ok && e1.Timeout() {
			return nil, ErrTimeout{err}
		}

		return nil, err

	case net.Error:
		// `http.Client.Do` will return a `net.Error` directly when Dial times
		// out, or when the Client's RoundTripper otherwise returns an err
		if err.Timeout() {
			return nil, ErrTimeout{err}
		}

		return nil, err
	}

	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/200 != 1 {
		return nil, errors.New(string(respBody))
	}
	return respBody, nil
}
