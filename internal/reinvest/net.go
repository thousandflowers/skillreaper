package reinvest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	userAgent      = "skillreaper-reinvest/0.1"
	defaultTimeout = 15 * time.Second
	maxResponse    = 5 * 1024 * 1024 // bytes read from any one answer; a bigger one fails to parse and is skipped
)

// httpError is an answer that was not a success.
type httpError struct {
	code       int
	status     string
	retryAfter string
}

func (e *httpError) Error() string { return "HTTP Error " + e.status }

// plainURL reports a URL every client reads the same way: printable ASCII
// only. Python's http.client refused the rest, so the prototype never
// requested them either.
func plainURL(url string) bool {
	for i := 0; i < len(url); i++ {
		if url[i] <= ' ' || url[i] >= 0x7f {
			return false
		}
	}
	return url != ""
}

// httpGet fetches a URL: at most maxResponse bytes of it.
func httpGet(url string, timeout time.Duration) ([]byte, error) {
	if !plainURL(url) {
		return nil, fmt.Errorf("the URL holds control characters or text outside ASCII: %q", url)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: timeout, CheckRedirect: noDowngrade}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, &httpError{code: res.StatusCode, status: res.Status, retryAfter: res.Header.Get("Retry-After")}
	}
	return io.ReadAll(io.LimitReader(res.Body, maxResponse))
}

// retryPolicy is how getJSON waits out a busy server.
type retryPolicy struct {
	tries   int
	retryOn []int // statuses worth waiting for
	maxWait int   // seconds; longer Retry-After values are capped
}

var (
	defaultRetry = retryPolicy{tries: 3, retryOn: []int{429, 500, 502, 503, 504}, maxWait: 8}
	onceMore     = retryPolicy{tries: 2, retryOn: defaultRetry.retryOn, maxWait: defaultRetry.maxWait}
)

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// retryWait is how long to wait before the next try: Retry-After when it is a
// number of seconds (it may be an HTTP date), else a doubling backoff.
func retryWait(header string, attempt int, maxWait int) (time.Duration, error) {
	wait := 0
	if text := pyStrip(header); text != "" {
		if n, err := strconv.Atoi(strings.ReplaceAll(text, "_", "")); err == nil {
			wait = n
		}
	}
	if wait < 0 {
		return 0, fmt.Errorf("a negative Retry-After: %s", header)
	}
	if wait == 0 {
		wait = 1 << (attempt + 1)
	}
	return time.Duration(min(wait, maxWait)) * time.Second, nil
}

// getJSON fetches and parses JSON, waiting out the policy's statuses. It
// returns what the last try returned.
func (a *app) getJSON(url string, policy retryPolicy) (any, error) {
	for attempt := 0; ; attempt++ {
		raw, err := a.httpGet(url, defaultTimeout)
		if err == nil {
			if !utf8.Valid(raw) {
				return nil, errors.New("the answer is not UTF-8 text")
			}
			v, err := parseJSON(string(raw))
			if err != nil { // a sign-in page, a proxy's notice
				return nil, fmt.Errorf("the answer was not JSON: %w", err)
			}
			return v, nil
		}
		var status *httpError
		if !errors.As(err, &status) || !containsInt(policy.retryOn, status.code) || attempt == policy.tries-1 {
			return nil, err
		}
		wait, err := retryWait(status.retryAfter, attempt, policy.maxWait)
		if err != nil {
			return nil, err
		}
		a.sleep(wait)
	}
}

var errTooManyRedirects = errors.New("stopped after 10 redirects")

// noDowngrade follows a redirect unless it leaves https: what was asked for
// over an encrypted connection is not read from a plain one.
func noDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errTooManyRedirects
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("a redirect from https to %s was not followed", req.URL.Scheme)
	}
	return nil
}
