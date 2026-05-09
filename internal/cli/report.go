package cli

import (
	"fmt"
	"io"
)

// CheckStatus is the bounded diagnostic state for a CLI check.
type CheckStatus string

const (
	// CheckPass indicates the check succeeded.
	CheckPass CheckStatus = "pass"
	// CheckWarn indicates an actionable warning that does not fail the command.
	CheckWarn CheckStatus = "warn"
	// CheckFail indicates the command should fail.
	CheckFail CheckStatus = "fail"
	// CheckSkip indicates the check was skipped because a prerequisite failed.
	CheckSkip CheckStatus = "skip"
)

// Check is one redacted, stable diagnostic result.
type Check struct {
	ID      string
	Title   string
	Status  CheckStatus
	Message string
}

// Report is a command diagnostic report.
type Report struct {
	Name   string
	Checks []Check
}

// Add appends one check result.
func (r *Report) Add(status CheckStatus, id string, title string, message string) {
	r.Checks = append(r.Checks, Check{
		ID:      id,
		Title:   title,
		Status:  status,
		Message: message,
	})
}

// Pass appends a passing check.
func (r *Report) Pass(id string, title string, message string) {
	r.Add(CheckPass, id, title, message)
}

// Warn appends a warning check.
func (r *Report) Warn(id string, title string, message string) {
	r.Add(CheckWarn, id, title, message)
}

// Fail appends a failing check.
func (r *Report) Fail(id string, title string, message string) {
	r.Add(CheckFail, id, title, message)
}

// Skip appends a skipped check.
func (r *Report) Skip(id string, title string, message string) {
	r.Add(CheckSkip, id, title, message)
}

// HasFailures reports whether any check failed.
func (r Report) HasFailures() bool {
	for _, check := range r.Checks {
		if check.Status == CheckFail {
			return true
		}
	}
	return false
}

// PrintText writes a stable human-readable report.
func PrintText(out io.Writer, report Report) {
	_, _ = fmt.Fprintf(out, "%s\n", report.Name)
	for _, check := range report.Checks {
		if check.Message == "" {
			_, _ = fmt.Fprintf(out, "[%s] %s: %s\n", check.Status, check.ID, check.Title)
			continue
		}
		_, _ = fmt.Fprintf(out, "[%s] %s: %s - %s\n", check.Status, check.ID, check.Title, check.Message)
	}
}
