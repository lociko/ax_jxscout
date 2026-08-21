package pipeline

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	chunkdiscoverer "github.com/lociko/ax_jxscout/internal/modules/chunk-discoverer"
)

var browserUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:141.0) Gecko/20100101 Firefox/141.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:141.0) Gecko/20100101 Firefox/141.0",
}

type Options struct {
	Concurrency          int
	RateLimitPerSecond   int
	RequestTimeout       time.Duration
	MaxResponseBytes     int64
	MaxAssetsPerSeed     int
	ChunkBruteForceLimit int
	AllowCrossOrigin     bool
	Headers              http.Header
}

type Record struct {
	SchemaVersion   int    `json:"schema_version"`
	Type            string `json:"type"`
	SeedURL         string `json:"seed_url,omitempty"`
	URL             string `json:"url,omitempty"`
	ParentURL       string `json:"parent_url,omitempty"`
	Relation        string `json:"relation,omitempty"`
	Stage           string `json:"stage,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	Size            int    `json:"size,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	ContentEncoding string `json:"content_encoding,omitempty"`
	Content         string `json:"content,omitempty"`
	Error           string `json:"error,omitempty"`
	Status          string `json:"status,omitempty"`
	Assets          int    `json:"assets,omitempty"`
	Errors          int    `json:"errors,omitempty"`
	Skipped         int    `json:"skipped,omitempty"`
}

type Runner struct {
	options    Options
	client     *http.Client
	discoverer *chunkdiscoverer.StandaloneDiscoverer
	limiter    *limiter
	encoder    *json.Encoder
	outputMu   sync.Mutex
	headers    http.Header
}

func New(options Options, output io.Writer) (*Runner, error) {
	if options.Concurrency <= 0 {
		return nil, errors.New("concurrency must be positive")
	}
	if options.RateLimitPerSecond < 0 {
		return nil, errors.New("rate limit must be non-negative")
	}
	if options.RequestTimeout <= 0 {
		return nil, errors.New("request timeout must be positive")
	}
	if options.MaxResponseBytes <= 0 {
		return nil, errors.New("max response bytes must be positive")
	}
	if options.MaxAssetsPerSeed <= 0 {
		return nil, errors.New("max assets per seed must be positive")
	}
	if options.ChunkBruteForceLimit < 0 {
		return nil, errors.New("chunk brute force limit must be non-negative")
	}

	discoverer, err := chunkdiscoverer.NewStandaloneDiscoverer()
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = max(20, options.Concurrency*4)
	transport.MaxIdleConnsPerHost = max(10, options.Concurrency*2)
	client := &http.Client{
		Transport: transport,
		Timeout:   options.RequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirected to unsupported scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	headers := options.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", browserUserAgents[rand.IntN(len(browserUserAgents))])
	}
	return &Runner{
		options:    options,
		client:     client,
		discoverer: discoverer,
		limiter:    newLimiter(options.RateLimitPerSecond),
		encoder:    encoder,
		headers:    headers,
	}, nil
}

func (r *Runner) Close() error {
	r.client.CloseIdleConnections()
	return r.discoverer.Close()
}

func (r *Runner) Run(ctx context.Context, seeds []string) error {
	sem := make(chan struct{}, r.options.Concurrency)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for _, rawSeed := range seeds {
		rawSeed := rawSeed
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if err := r.crawlSeed(ctx, rawSeed); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

type candidate struct {
	url      string
	parent   string
	relation string
}

type crawlStats struct {
	assets    int
	errors    int
	skipped   int
	truncated bool
}

func (r *Runner) crawlSeed(ctx context.Context, rawSeed string) error {
	seed, err := parseHTTPURL(rawSeed)
	if err != nil {
		if emitErr := r.emitError(rawSeed, rawSeed, "input", err, 0); emitErr != nil {
			return emitErr
		}
		return r.emitSummary(rawSeed, crawlStats{errors: 1})
	}
	seed.Fragment = ""
	seedURL := seed.String()
	stats := crawlStats{}
	allowedHosts := map[string]bool{strings.ToLower(seed.Host): true}

	response, err := r.fetch(ctx, seedURL)
	if err != nil {
		stats.errors++
		if emitErr := r.emitError(seedURL, seedURL, "fetch-seed", err, response.statusCode); emitErr != nil {
			return emitErr
		}
		return r.emitSummary(seedURL, stats)
	}
	if effective, parseErr := url.Parse(response.url); parseErr == nil {
		allowedHosts[strings.ToLower(effective.Host)] = true
	}

	temporary, err := os.MkdirTemp("", "ax-jxscout-pipe-*")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(temporary)

	queue := make([]candidate, 0)
	if isHTML(response.contentType, response.body) {
		scripts, extractErr := extractScriptURLs(response.body, response.url)
		if extractErr != nil {
			stats.errors++
			if emitErr := r.emitError(seedURL, response.url, "parse-html", extractErr, response.statusCode); emitErr != nil {
				return emitErr
			}
		} else {
			for _, script := range scripts {
				queue = append(queue, candidate{url: script, parent: response.url, relation: "html-script"})
			}
		}
	} else {
		queue = append(queue, candidate{url: response.url, relation: "seed-javascript"})
	}

	seen := make(map[string]bool)
	for len(queue) > 0 {
		if stats.assets >= r.options.MaxAssetsPerSeed {
			stats.truncated = true
			break
		}
		item := queue[0]
		queue = queue[1:]
		assetURL, parseErr := parseHTTPURL(item.url)
		if parseErr != nil {
			stats.errors++
			if emitErr := r.emitError(seedURL, item.url, "resolve-url", parseErr, 0); emitErr != nil {
				return emitErr
			}
			continue
		}
		assetURL.Fragment = ""
		canonical := assetURL.String()
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		if !r.options.AllowCrossOrigin && !allowedHosts[strings.ToLower(assetURL.Host)] {
			stats.skipped++
			continue
		}

		var assetResponse httpResponse
		if canonical == response.url && item.relation == "seed-javascript" {
			assetResponse = response
		} else {
			assetResponse, err = r.fetch(ctx, canonical)
			if err != nil {
				stats.errors++
				if emitErr := r.emitError(seedURL, canonical, "fetch-javascript", err, assetResponse.statusCode); emitErr != nil {
					return emitErr
				}
				continue
			}
		}
		if isHTML(assetResponse.contentType, assetResponse.body) {
			stats.errors++
			if emitErr := r.emitError(seedURL, canonical, "validate-javascript", errors.New("response looks like HTML"), assetResponse.statusCode); emitErr != nil {
				return emitErr
			}
			continue
		}

		if err := r.emitAsset(seedURL, item, assetResponse); err != nil {
			return err
		}
		stats.assets++

		javascriptPath := fmt.Sprintf("%s/%06d.js", temporary, stats.assets)
		if err := os.WriteFile(javascriptPath, assetResponse.body, 0600); err != nil {
			return fmt.Errorf("write temporary javascript: %w", err)
		}
		chunks, discoverErr := r.discoverer.Discover(ctx, javascriptPath, r.options.ChunkBruteForceLimit)
		if discoverErr != nil {
			stats.errors++
			if emitErr := r.emitError(seedURL, assetResponse.url, "discover-chunks", discoverErr, assetResponse.statusCode); emitErr != nil {
				return emitErr
			}
			continue
		}
		for _, chunk := range chunks {
			resolved, resolveErr := resolveChunkURL(assetResponse.url, chunk)
			if resolveErr != nil {
				stats.errors++
				if emitErr := r.emitError(seedURL, chunk, "resolve-chunk", resolveErr, 0); emitErr != nil {
					return emitErr
				}
				continue
			}
			queue = append(queue, candidate{url: resolved, parent: assetResponse.url, relation: "discovered-chunk"})
		}
	}
	return r.emitSummary(seedURL, stats)
}

type httpResponse struct {
	url         string
	statusCode  int
	contentType string
	body        []byte
}

func (r *Runner) fetch(ctx context.Context, rawURL string) (httpResponse, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return httpResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return httpResponse{}, err
	}
	req.Header.Set("Accept", "text/html,application/javascript,text/javascript,*/*;q=0.8")
	for name, values := range r.headers {
		req.Header.Del(name)
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return httpResponse{}, err
	}
	defer resp.Body.Close()
	result := httpResponse{url: resp.Request.URL.String(), statusCode: resp.StatusCode, contentType: resp.Header.Get("Content-Type")}
	body, err := io.ReadAll(io.LimitReader(resp.Body, r.options.MaxResponseBytes+1))
	if err != nil {
		return result, err
	}
	if int64(len(body)) > r.options.MaxResponseBytes {
		return result, fmt.Errorf("response exceeds %d bytes", r.options.MaxResponseBytes)
	}
	result.body = body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	return result, nil
}

func (r *Runner) emit(record Record) error {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	return r.encoder.Encode(record)
}

func (r *Runner) emitAsset(seedURL string, item candidate, response httpResponse) error {
	digest := sha256.Sum256(response.body)
	compressed, err := compress(response.body)
	if err != nil {
		return err
	}
	return r.emit(Record{
		SchemaVersion:   1,
		Type:            "asset",
		SeedURL:         seedURL,
		URL:             response.url,
		ParentURL:       item.parent,
		Relation:        item.relation,
		StatusCode:      response.statusCode,
		ContentType:     response.contentType,
		Size:            len(response.body),
		SHA256:          hex.EncodeToString(digest[:]),
		ContentEncoding: "gzip+base64",
		Content:         base64.StdEncoding.EncodeToString(compressed),
	})
}

func (r *Runner) emitError(seedURL, assetURL, stage string, err error, statusCode int) error {
	return r.emit(Record{
		SchemaVersion: 1,
		Type:          "error",
		SeedURL:       seedURL,
		URL:           assetURL,
		Stage:         stage,
		StatusCode:    statusCode,
		Error:         err.Error(),
	})
}

func (r *Runner) emitSummary(seedURL string, stats crawlStats) error {
	status := "complete"
	if stats.truncated {
		status = "truncated"
	} else if stats.errors > 0 && stats.assets == 0 {
		status = "failed"
	} else if stats.errors > 0 {
		status = "complete_with_errors"
	}
	return r.emit(Record{
		SchemaVersion: 1,
		Type:          "summary",
		SeedURL:       seedURL,
		Status:        status,
		Assets:        stats.assets,
		Errors:        stats.errors,
		Skipped:       stats.skipped,
	})
}

func parseHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("URL must include a host")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("URL userinfo is not supported")
	}
	return parsed, nil
}

func extractScriptURLs(body []byte, rawBaseURL string) ([]string, error) {
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, err
	}
	if href, exists := document.Find("base[href]").First().Attr("href"); exists {
		if parsed, parseErr := url.Parse(strings.TrimSpace(href)); parseErr == nil {
			baseURL = baseURL.ResolveReference(parsed)
		}
	}

	seen := make(map[string]bool)
	var scripts []string
	add := func(reference string) {
		parsed, parseErr := url.Parse(strings.TrimSpace(reference))
		if parseErr != nil || parsed.Scheme == "data" || parsed.Scheme == "javascript" || parsed.Scheme == "blob" {
			return
		}
		resolved := baseURL.ResolveReference(parsed)
		resolved.Fragment = ""
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}
		value := resolved.String()
		if !seen[value] {
			seen[value] = true
			scripts = append(scripts, value)
		}
	}
	document.Find("script[src]").Each(func(_ int, selection *goquery.Selection) {
		if source, exists := selection.Attr("src"); exists {
			add(source)
		}
	})
	document.Find("link[href]").Each(func(_ int, selection *goquery.Selection) {
		relation, _ := selection.Attr("rel")
		as, _ := selection.Attr("as")
		if strings.EqualFold(relation, "modulepreload") || (strings.EqualFold(relation, "preload") && strings.EqualFold(as, "script")) {
			if href, exists := selection.Attr("href"); exists {
				add(href)
			}
		}
	})
	sort.Strings(scripts)
	return scripts, nil
}

func resolveChunkURL(parentRawURL, chunk string) (string, error) {
	parent, err := parseHTTPURL(parentRawURL)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(strings.TrimSpace(chunk))
	if err != nil {
		return "", err
	}
	if reference.Scheme == "data" || reference.Scheme == "javascript" || reference.Scheme == "blob" {
		return "", fmt.Errorf("unsupported chunk URL scheme %q", reference.Scheme)
	}
	if reference.IsAbs() || reference.Host != "" || strings.HasPrefix(reference.Path, "/") || strings.HasPrefix(reference.Path, "./") || strings.HasPrefix(reference.Path, "../") {
		resolved := parent.ResolveReference(reference)
		resolved.Fragment = ""
		return resolved.String(), nil
	}

	firstPart := ""
	for _, part := range strings.Split(reference.Path, "/") {
		if part != "" {
			firstPart = part
			break
		}
	}
	parentParts := strings.Split(parent.Path, "/")
	commonIndex := -1
	for index := len(parentParts) - 1; index >= 0; index-- {
		if firstPart != "" && parentParts[index] == firstPart {
			commonIndex = index
			break
		}
	}
	if commonIndex == -1 {
		resolved := parent.ResolveReference(reference)
		resolved.Fragment = ""
		return resolved.String(), nil
	}
	resolved := *parent
	resolved.Path = path.Join(append(parentParts[:commonIndex], reference.Path)...)
	if strings.HasPrefix(parent.Path, "/") && !strings.HasPrefix(resolved.Path, "/") {
		resolved.Path = "/" + resolved.Path
	}
	resolved.RawPath = ""
	resolved.RawQuery = reference.RawQuery
	resolved.Fragment = ""
	return resolved.String(), nil
}

func isHTML(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "html") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	return bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<!doctype html")) || bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<html"))
}

func compress(body []byte) ([]byte, error) {
	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	writer.Header.ModTime = time.Unix(0, 0)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newLimiter(requestsPerSecond int) *limiter {
	if requestsPerSecond <= 0 {
		return &limiter{}
	}
	return &limiter{interval: time.Second / time.Duration(requestsPerSecond)}
}

func (l *limiter) Wait(ctx context.Context) error {
	if l.interval == 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	ready := l.next
	if ready.Before(now) {
		ready = now
	}
	l.next = ready.Add(l.interval)
	l.mu.Unlock()
	if delay := time.Until(ready); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
