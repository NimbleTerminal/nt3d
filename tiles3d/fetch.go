// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Fetcher returns the bytes a URI names: a tileset, a subtree, or a tile's
// content. It is how everything outside the program is reached, so a cache,
// a test, or an archive is a Fetcher too. Fetch must be safe to call from
// several goroutines, and should give up when ctx is done.
type Fetcher interface {
	Fetch(ctx context.Context, uri string) ([]byte, error)
}

// FetcherFunc is a function as a Fetcher.
type FetcherFunc func(ctx context.Context, uri string) ([]byte, error)

// Fetch calls f.
func (f FetcherFunc) Fetch(ctx context.Context, uri string) ([]byte, error) { return f(ctx, uri) }

// Resolve returns the URI that ref means inside the document at base: ref
// itself when it is absolute, and otherwise ref beside base, as a link is
// resolved on a web page. base and ref may be URLs or slash-separated paths.
// A data: URI, and a ref that climbs above a path base's root, are errors.
func Resolve(base, ref string) (string, error) {
	if len(ref) > maxURILength || len(base) > maxURILength {
		return "", fmt.Errorf("%w: uri of %d bytes", ErrTooLarge, max(len(ref), len(base)))
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("nt3dtiles3d: uri: %w", err)
	}
	if r.Scheme == "data" {
		return "", errors.New("nt3dtiles3d: data URIs are not supported")
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("nt3dtiles3d: base uri: %w", err)
	}
	if r.IsAbs() || b.IsAbs() {
		return b.ResolveReference(r).String(), nil
	}
	// Two plain paths: join them, and keep the result under the root.
	p := r.Path
	if !strings.HasPrefix(p, "/") {
		p = path.Join(path.Dir(b.Path), p)
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", errors.New("nt3dtiles3d: uri climbs above its root")
	}
	return p, nil
}

// FS returns a Fetcher that reads slash-separated paths from fsys, such as
// os.DirFS of a directory holding a tileset, or an embedded one. A leading
// slash is dropped, and a query or fragment ignored.
func FS(fsys fs.FS) Fetcher {
	return FetcherFunc(func(ctx context.Context, uri string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i := strings.IndexAny(uri, "?#"); i >= 0 {
			uri = uri[:i]
		}
		if unescaped, err := url.PathUnescape(uri); err == nil {
			uri = unescaped
		}
		name := path.Clean(strings.TrimPrefix(uri, "/"))
		if !fs.ValidPath(name) {
			return nil, fmt.Errorf("nt3dtiles3d: %q is not a path in the file system", printable(uri, 128))
		}
		return fs.ReadFile(fsys, name)
	})
}

// HTTP is a Fetcher that asks a web server, natively and in a browser build
// alike. The zero value works.
type HTTP struct {
	// Client makes the requests; http.DefaultClient when nil.
	Client *http.Client
	// Prepare, when set, sees each request before it is sent, to add
	// headers. Bearer returns one that adds a token.
	Prepare func(*http.Request)
	// MaxBytes is the most bytes one response may have: 256 MiB when zero.
	// A longer one is an error that wraps ErrTooLarge.
	MaxBytes int64
}

// Fetch gets uri, which must be an http or https URL.
func (h *HTTP) Fetch(ctx context.Context, uri string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("nt3dtiles3d: %w", err)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("nt3dtiles3d: cannot fetch a %q uri over HTTP", printable(req.URL.Scheme, 16))
	}
	if h != nil && h.Prepare != nil {
		h.Prepare(req)
	}
	client := http.DefaultClient
	if h != nil && h.Client != nil {
		client = h.Client
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nt3dtiles3d: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nt3dtiles3d: %s: %s", printable(req.URL.Redacted(), 256), printable(resp.Status, 64))
	}
	limit := int64(256 << 20)
	if h != nil && h.MaxBytes > 0 {
		limit = h.MaxBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("nt3dtiles3d: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: response over %d bytes", ErrTooLarge, limit)
	}
	return data, nil
}

// Bearer returns a Prepare for HTTP that sends token as a bearer token, and
// only where it is safe to: to an https URL on the same host as origin, the
// tileset's own URL. A tile that links to another server, or to plain http,
// is fetched without it.
func Bearer(token, origin string) func(*http.Request) {
	home, err := url.Parse(origin)
	return func(req *http.Request) {
		if err != nil || token == "" || req.URL.Scheme != "https" || home.Scheme != "https" || !strings.EqualFold(req.URL.Host, home.Host) {
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
}
