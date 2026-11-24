package services

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxRSSFeedSize = 10 << 20 // 10 MiB, consistent with the outbound cover-art limit.

var errRSSFeedTooLarge = errors.New("rss feed response exceeds size limit")

// withRSSFeedSizeLimit copies client configuration and caps every RSS response
// body before gofeed parses it. The copy avoids changing a client owned by the caller.
func withRSSFeedSizeLimit(client *http.Client) *http.Client {
	clientCopy := *client
	transport := clientCopy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clientCopy.Transport = &rssFeedSizeLimitTransport{next: transport}
	return &clientCopy
}

type rssFeedSizeLimitTransport struct {
	next http.RoundTripper
}

func (t *rssFeedSizeLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.ContentLength > maxRSSFeedSize {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("%w: content length %d exceeds %d", errRSSFeedTooLarge, resp.ContentLength, maxRSSFeedSize)
	}
	if resp.Body != nil {
		resp.Body = &rssFeedSizeLimitBody{ReadCloser: resp.Body, remaining: maxRSSFeedSize}
	}
	return resp, nil
}

type rssFeedSizeLimitBody struct {
	io.ReadCloser
	remaining int64
	exceeded  bool
}

func (b *rssFeedSizeLimitBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.exceeded {
		return 0, errRSSFeedTooLarge
	}
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n > 0 {
			b.exceeded = true
			return 0, errRSSFeedTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
