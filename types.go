package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/montanaflynn/stats"
)

const (
	dateFormat             = time.RFC1123
	numHistoricalDurations = 300

	statusTemplateStr = `[{{ .Start.Format "Mon, 02 Jan 2006 15:04:05 MST" }}] ({{ .Status }}) {{ .Message }} ({{ .DurationStats }}){{ if ne .DumpFile "" }} (Resp: {{ .DumpFile }}){{ end }}`
)

const (
	statusUnknown checkStatus = iota
	statusOk
	statusFailed
)

type (
	checkResult struct {
		DumpFile  string
		Durations *ringDuration
		Message   string
		Start     time.Time
		Status    checkStatus

		lock        sync.RWMutex
		lastLineLen int
	}

	checkStatus uint
)

var statusTemplate *template.Template

func init() {
	var err error
	if statusTemplate, err = template.New("statusTemplate").Parse(statusTemplateStr); err != nil {
		panic(fmt.Errorf("parsing status template: %w", err))
	}
}

func newCheckResult(status checkStatus, message string, duration time.Duration) *checkResult {
	r := newRingDuration(numHistoricalDurations)
	r.SetNext(duration)

	return &checkResult{
		Durations: r,
		Message:   message,
		Start:     time.Now(),
		Status:    status,
	}
}

func (c *checkResult) AddDuration(d time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.Durations.SetNext(d)
}

func (c *checkResult) DurationStats() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	var (
		s             = stats.LoadRawData(c.Durations.GetAll())
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

func (c *checkResult) Print() (err error) {
	buf := new(bytes.Buffer)
	if err = statusTemplate.Execute(buf, c); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	if c.lastLineLen > 0 {
		if _, err = fmt.Fprintf(os.Stdout, "\r%s\r", strings.Repeat(" ", c.lastLineLen)); err != nil {
			return fmt.Errorf("clearing status: %w", err)
		}
	}

	c.lastLineLen = buf.Len()
	if _, err = buf.WriteTo(os.Stdout); err != nil {
		return fmt.Errorf("printing status: %w", err)
	}

	return nil
}

func (c checkStatus) String() string {
	return map[checkStatus]string{
		statusUnknown: "UNKN",
		statusFailed:  "FAIL",
		statusOk:      "OKAY",
	}[c]
}
