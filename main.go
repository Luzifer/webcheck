package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Luzifer/rconfig"
	"github.com/montanaflynn/stats"
	log "github.com/sirupsen/logrus"
)

const dateFormat = time.RFC1123

var (
	cfg = struct {
		DisableLog     bool          `flag:"no-log" default:"false" description:"Disable response body logging"`
		Interval       time.Duration `flag:"interval,i" default:"1s" description:"Check interval"`
		LogDir         string        `flag:"log-dir,l" default:"./request-log/" description:"Directory to log non-matched requests to"`
		LogRetention   time.Duration `flag:"log-retention" default:"24h" description:"When to clean up file from log-dir"`
		Match          string        `flag:"match,m" default:".*" description:"RegExp to match the response body against to validate it"`
		Timeout        time.Duration `flag:"timeout,t" default:"30s" description:"Timeout for the request"`
		URL            string        `flag:"url,u" default:"" description:"URL to query" validate:"nonzero"`
		VersionAndExit bool          `flag:"version" default:"false" description:"Prints current version and exits"`
	}{}

	version = "dev"
)

type checkStatus uint

func (c checkStatus) String() string {
	return map[checkStatus]string{
		statusUnknown: "UNKN",
		statusFailed:  "FAIL",
		statusOk:      "OKAY",
	}[c]
}

const (
	statusUnknown checkStatus = iota
	statusOk
	statusFailed
)

type checkResult struct {
	DumpFile  string
	Durations []time.Duration
	Message   string
	Start     time.Time
	Status    checkStatus

	lock        sync.RWMutex
	lastLineLen int
}

func newCheckResult(status checkStatus, message string, duration time.Duration) *checkResult {
	return &checkResult{
		Durations: []time.Duration{duration},
		Message:   message,
		Start:     time.Now(),
		Status:    status,
	}
}

func (c *checkResult) AddDuration(d time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.Durations = append(c.Durations, d)
}

func (c *checkResult) DurationStats() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	var (
		s             = stats.LoadRawData(c.Durations)
		min, avg, max float64
		err           error
	)
	if min, err = s.Min(); err != nil {
		min = 0
	}
	if avg, err = s.Median(); err != nil {
		avg = 0
	}
	if max, err = s.Max(); err != nil {
		max = 0
	}

	return fmt.Sprintf("%s/%s/%s",
		time.Duration(min).Round(time.Microsecond).String(),
		time.Duration(avg).Round(time.Microsecond).String(),
		time.Duration(max).Round(time.Microsecond).String(),
	)
}

func (c *checkResult) Equals(r *checkResult) bool {
	return c.Status == r.Status && c.Message == r.Message
}

func (c *checkResult) Print() {
	tpl := strings.Join([]string{
		`[{{ .Start.Format "` + dateFormat + `" }}]`,
		`({{ .Status }})`,
		`{{ .Message }}`,
		`({{ .DurationStats }})`,
		`{{ if ne .DumpFile "" }}(Resp: {{ .DumpFile }}){{ end }}`,
	}, " ")
	templ := template.Must(template.New("result").Parse(tpl))

	buf := new(bytes.Buffer)
	templ.Execute(buf, c)

	if c.lastLineLen > 0 {
		fmt.Fprintf(os.Stdout, "\r%s\r", strings.Repeat(" ", c.lastLineLen))
	}

	c.lastLineLen = buf.Len()
	buf.WriteTo(os.Stdout)
}

func init() {
	if err := rconfig.ParseAndValidate(&cfg); err != nil {
		log.Fatalf("Unable to parse commandline options: %s", err)
	}

	if cfg.VersionAndExit {
		fmt.Printf("webcheck %s\n", version)
		os.Exit(0)
	}
}

func main() {
	http.DefaultClient.Timeout = cfg.Timeout
	matcher, err := regexp.Compile(cfg.Match)
	if err != nil {
		log.WithError(err).Fatal("Matcher regexp does not compile")
	}

	lastResult := newCheckResult(statusUnknown, "Uninitialized", 0)

	for range time.Tick(cfg.Interval) {
		var (
			body   *bytes.Buffer
			result *checkResult
		)

		if !cfg.DisableLog {
			body = new(bytes.Buffer)
		}

		result = doCheck(cfg.URL, matcher, body)

		if !result.Equals(lastResult) {
			fmt.Println()
			lastResult = result

			if result.Status == statusFailed {
				fn, err := dumpRequest(body)
				if err != nil {
					log.WithError(err).Fatal("Could not dump request")
				}
				lastResult.DumpFile = fn
			}
		} else {
			lastResult.AddDuration(result.Durations[0])
		}

		lastResult.Print()
	}
}

func doCheck(url string, match *regexp.Regexp, responseBody io.Writer) *checkResult {
	req, _ := http.NewRequest("GET", url, nil)

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
	defer resp.Body.Close()

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
		fmt.Fprintln(responseBody)
		if _, err = responseBody.Write(body.Bytes()); err != nil {
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

	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		return "", err
	}

	f, err := ioutil.TempFile(cfg.LogDir, "request")
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = io.Copy(f, body)

	return f.Name(), err
}
