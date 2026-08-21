package chunkdiscoverer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// StandaloneDiscoverer exposes the existing embedded chunk-discovery program
// without requiring the proxy server, database, or event bus.
type StandaloneDiscoverer struct {
	path string
}

func NewStandaloneDiscoverer() (*StandaloneDiscoverer, error) {
	file, err := os.CreateTemp("", "ax-jxscout-chunk-discoverer-*.js")
	if err != nil {
		return nil, fmt.Errorf("create chunk discoverer: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(chunkDiscovererBinary); err != nil {
		file.Close()
		os.Remove(path)
		return nil, fmt.Errorf("write chunk discoverer: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("close chunk discoverer: %w", err)
	}
	if err := os.Chmod(path, 0700); err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("chmod chunk discoverer: %w", err)
	}
	return &StandaloneDiscoverer{path: path}, nil
}

func (d *StandaloneDiscoverer) Close() error {
	if d == nil || d.path == "" {
		return nil
	}
	return os.Remove(d.path)
}

func (d *StandaloneDiscoverer) Discover(ctx context.Context, javascriptPath string, bruteForceLimit int) ([]string, error) {
	if bruteForceLimit < 0 {
		return nil, fmt.Errorf("brute force limit must be non-negative")
	}
	cmd := exec.CommandContext(ctx, "bun", "run", d.path, javascriptPath, strconv.Itoa(bruteForceLimit))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("run chunk discoverer: %s", message)
	}

	var chunks []string
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		chunk := strings.TrimSpace(scanner.Text())
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read chunk discoverer output: %w", err)
	}
	return chunks, nil
}
