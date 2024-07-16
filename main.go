package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Luzifer/rconfig/v2"
)

const (
	cleanupInterval = 10 * time.Second
	logFolderPerms  = 0o750
)

var (
	cfg = struct {
		DisableLog     bool          `flag:"no-log" default:"false" description:"Disable response body logging"`
		Interval       time.Duration `flag:"interval,i" default:"1s" description:"Check interval"`
		LogDir         string        `flag:"log-dir,l" default:"/tmp/resp-log/" description:"Directory to log non-matched requests to"`
		LogLevel       string        `flag:"log-level" default:"info" description:"Log level (debug, info, warn, error, fatal)"`
		LogRetention   time.Duration `flag:"log-retention" default:"24h" description:"When to clean up file from log-dir"`
		Match          string        `flag:"match,m" default:".*" description:"RegExp to match the response body against to validate it"`
		Timeout        time.Duration `flag:"timeout,t" default:"30s" description:"Timeout for the request"`
		URL            string        `flag:"url,u" default:"" description:"URL to query" validate:"nonzero"`
		VersionAndExit bool          `flag:"version" default:"false" description:"Prints current version and exits"`
	}{}

	version = "dev"
)

func initApp() error {
	rconfig.AutoEnv(true)
	if err := rconfig.ParseAndValidate(&cfg); err != nil {
		return fmt.Errorf("parsing cli options: %w", err)
	}

	l, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("parsing log-level: %w", err)
	}
	logrus.SetLevel(l)

	return nil
}

func main() {
	var err error
	if err = initApp(); err != nil {
		logrus.WithError(err).Fatal("initializing app")
	}

	if cfg.VersionAndExit {
		logrus.WithField("version", version).Info("webcheck")
		os.Exit(0)
	}

	matcher, err := regexp.Compile(cfg.Match)
	if err != nil {
		logrus.WithError(err).Fatal("compiling matcher RegExp")
	}

	lastResult := newCheckResult(statusUnknown, "Uninitialized", 0)

	go cleanupLogFiles()

	for range time.Tick(cfg.Interval) {
		var (
			body   io.ReadWriter
			result *checkResult
		)

		if !cfg.DisableLog {
			body = new(bytes.Buffer)
		}

		result = doCheck(cfg.URL, matcher, body)

		if !result.Equals(lastResult) {
			fmt.Println() //nolint:forbidigo
			lastResult = result

			if result.Status == statusFailed {
				fn, err := dumpRequest(body)
				if err != nil {
					logrus.WithError(err).Fatal("logging request")
				}
				lastResult.DumpFile = fn
			}
		} else {
			lastResult.AddDuration(result.Durations.GetCurrent())
		}

		if err = lastResult.Print(); err != nil {
			logrus.WithError(err).Fatal("displaying status")
		}
	}
}

func cleanupLogFiles() {
	for range time.Tick(cleanupInterval) {
		if info, err := os.Stat(cfg.LogDir); err != nil || !info.IsDir() {
			continue
		}

		if err := filepath.Walk(cfg.LogDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && time.Since(info.ModTime()) > cfg.LogRetention {
				return os.Remove(path)
			}

			return nil
		}); err != nil {
			fmt.Println() //nolint:forbidigo
			logrus.WithError(err).Error("cleaning up logs")
		}
	}
}

func doCheck(url string, match *regexp.Regexp, responseBody io.Writer) *checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	respStart := time.Now()
	resp, err := http.DefaultClient.Do(req)
	respDuration := time.Since(respStart)
	if err != nil {
		return newCheckResult(
			statusFailed,
			fmt.Sprintf("HTTP request failed: %s", err),
			respDuration,
		)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logrus.WithError(err).Error("closing response body (leaked fd)")
		}
	}()

	body := new(bytes.Buffer)
	if _, err = io.Copy(body, resp.Body); err != nil {
		return newCheckResult(
			statusFailed,
			"Was not able to read response body",
			respDuration,
		)
	}

	if responseBody != nil {
		if err = resp.Header.Write(responseBody); err != nil {
			return newCheckResult(
				statusFailed,
				"Was not able to copy headers",
				respDuration,
			)
		}

		if _, err = responseBody.Write(append([]byte{'\n'}, body.Bytes()...)); err != nil {
			return newCheckResult(
				statusFailed,
				"Was not able to copy body",
				respDuration,
			)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newCheckResult(
			statusFailed,
			fmt.Sprintf("Status code was != 2xx: %d", resp.StatusCode),
			respDuration,
		)
	}

	if !match.Match(body.Bytes()) {
		return newCheckResult(
			statusFailed,
			"Response body does not match regexp",
			respDuration,
		)
	}

	return newCheckResult(
		statusOk,
		fmt.Sprintf("Status was %d and text matched", resp.StatusCode),
		respDuration,
	)
}

func dumpRequest(body io.Reader) (string, error) {
	if body == nil {
		return "", nil
	}

	if err := os.MkdirAll(cfg.LogDir, logFolderPerms); err != nil {
		return "", fmt.Errorf("creating log folder: %w", err)
	}

	f, err := os.CreateTemp(cfg.LogDir, "resp")
	if err != nil {
		return "", fmt.Errorf("creating log file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			logrus.WithError(err).Error("closing log file (leaked fd)")
		}
	}()

	if _, err = io.Copy(f, body); err != nil {
		return "", fmt.Errorf("copying request body: %w", err)
	}

	return f.Name(), nil
}
