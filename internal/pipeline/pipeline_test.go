package pipeline

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func TestExtractScriptURLsUsesDocumentBase(t *testing.T) {
	html := []byte(`<html><head><base href="/assets/"><script src="main.js"></script><link rel="modulepreload" href="chunk.js"></head></html>`)
	got, err := extractScriptURLs(html, "https://example.test/app/page")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.test/assets/chunk.js", "https://example.test/assets/main.js"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestResolveChunkURLPreservesOriginPortAndQuery(t *testing.T) {
	got, err := resolveChunkURL("https://example.test:8443/static/chunks/main.js", "static/chunks/12.js?v=4")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.test:8443/static/chunks/12.js?v=4" {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestRunnerFetchesHTMLScriptsAndEmitsRecoverableContent(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not installed")
	}
	javascript := []byte(`console.log("pipeline fixture");`)
	var receivedHeaders []http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedHeaders = append(receivedHeaders, request.Header.Clone())
		switch request.URL.Path {
		case "/":
			writer.Header().Set("Content-Type", "text/html")
			io.WriteString(writer, `<script src="/app.js"></script>`)
		case "/app.js":
			writer.Header().Set("Content-Type", "application/javascript")
			writer.Write(javascript)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	runner, err := New(Options{
		Concurrency:          1,
		RateLimitPerSecond:   0,
		RequestTimeout:       5 * time.Second,
		MaxResponseBytes:     1024 * 1024,
		MaxAssetsPerSeed:     10,
		ChunkBruteForceLimit: 10,
		Headers:              http.Header{"X-Test": []string{"pipeline"}},
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	if err := runner.Run(context.Background(), []string{server.URL}); err != nil {
		t.Fatal(err)
	}

	var records []Record
	scanner := bufio.NewScanner(&output)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Type != "asset" || records[1].Type != "summary" {
		t.Fatalf("unexpected records: %#v", records)
	}
	decoded, err := base64.StdEncoding.DecodeString(records[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, javascript) {
		t.Fatalf("got %q, want %q", recovered, javascript)
	}
	parsed, _ := url.Parse(records[0].URL)
	if parsed.Path != "/app.js" || records[1].Assets != 1 || records[1].Status != "complete" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if len(receivedHeaders) != 2 {
		t.Fatalf("expected headers on two requests, got %d", len(receivedHeaders))
	}
	userAgent := receivedHeaders[0].Get("User-Agent")
	if !slices.Contains(browserUserAgents, userAgent) {
		t.Fatalf("unexpected default user agent: %q", userAgent)
	}
	for _, headers := range receivedHeaders {
		if headers.Get("User-Agent") != userAgent || headers.Get("X-Test") != "pipeline" {
			t.Fatalf("headers were not propagated consistently: %#v", headers)
		}
	}
}

func TestExplicitUserAgentOverridesDefault(t *testing.T) {
	runner, err := New(Options{
		Concurrency:          1,
		RequestTimeout:       time.Second,
		MaxResponseBytes:     1024,
		MaxAssetsPerSeed:     1,
		ChunkBruteForceLimit: 0,
		Headers:              http.Header{"User-Agent": []string{"explicit-agent"}},
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	if got := runner.headers.Get("User-Agent"); got != "explicit-agent" {
		t.Fatalf("got %q, want explicit-agent", got)
	}
}
