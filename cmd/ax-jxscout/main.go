package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lociko/ax_jxscout/internal/pipeline"
)

const version = "0.1.0"

func main() {
	var (
		inputFile       string
		concurrency     int
		rateLimit       int
		requestTimeout  time.Duration
		maxBodyBytes    int64
		maxAssets       int
		bruteForceLimit int
		allowCross      bool
		showVersion     bool
	)
	flag.StringVar(&inputFile, "input-file", "-", "read seed URLs from this file instead of stdin (- means stdin)")
	flag.IntVar(&concurrency, "concurrency", 2, "number of seed URLs to process concurrently")
	flag.IntVar(&rateLimit, "rate-limit", 2, "maximum HTTP requests per second for this process (0 means unlimited)")
	flag.DurationVar(&requestTimeout, "request-timeout", 30*time.Second, "timeout for each HTTP request")
	flag.Int64Var(&maxBodyBytes, "max-response-bytes", 15*1024*1024, "maximum bytes accepted from one response")
	flag.IntVar(&maxAssets, "max-assets-per-seed", 500, "maximum JavaScript assets fetched for one seed URL")
	flag.IntVar(&bruteForceLimit, "chunk-bruteforce-limit", 3000, "maximum chunk IDs tested by the inherited chunk detector")
	flag.BoolVar(&allowCross, "allow-cross-origin", false, "fetch scripts and chunks from hosts other than the seed/redirect host")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("ax-jxscout %s\n", version)
		return
	}

	seeds, err := readSeeds(inputFile, flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ax-jxscout: %s\n", err)
		os.Exit(2)
	}
	if len(seeds) == 0 {
		return
	}

	runner, err := pipeline.New(pipeline.Options{
		Concurrency:          concurrency,
		RateLimitPerSecond:   rateLimit,
		RequestTimeout:       requestTimeout,
		MaxResponseBytes:     maxBodyBytes,
		MaxAssetsPerSeed:     maxAssets,
		ChunkBruteForceLimit: bruteForceLimit,
		AllowCrossOrigin:     allowCross,
	}, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ax-jxscout: %s\n", err)
		os.Exit(2)
	}
	defer runner.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(ctx, seeds); err != nil {
		fmt.Fprintf(os.Stderr, "ax-jxscout: %s\n", err)
		if ctx.Err() != nil {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func readSeeds(inputFile string, arguments []string) ([]string, error) {
	seeds := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if value := strings.TrimSpace(argument); value != "" {
			seeds = append(seeds, value)
		}
	}
	if inputFile == "-" && len(arguments) > 0 {
		return seeds, nil
	}

	var source io.Reader = os.Stdin
	var file *os.File
	if inputFile != "-" {
		var err error
		file, err = os.Open(inputFile)
		if err != nil {
			return nil, fmt.Errorf("open input file: %w", err)
		}
		defer file.Close()
		source = file
	}

	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		value := strings.TrimSpace(scanner.Text())
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		seeds = append(seeds, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read seed URLs: %w", err)
	}
	return seeds, nil
}
