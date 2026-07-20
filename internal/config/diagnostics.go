package config

import (
	"fmt"
	"io"
	"strings"
)

// Severity classifies a diagnostic. Errors block startup; warnings do not.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is one configuration problem. Requirement R1 and acceptance B4
// require every diagnostic to identify the field and the remedy.
type Diagnostic struct {
	Severity Severity
	// Field is the TOML path, such as "security.writable[2]".
	Field string
	// Message states what is wrong.
	Message string
	// Remedy states what to do about it.
	Remedy string
}

func (d Diagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", d.Severity, d.Field)
	if d.Message != "" {
		fmt.Fprintf(&b, "\n    %s", d.Message)
	}
	if d.Remedy != "" {
		fmt.Fprintf(&b, "\n    remedy: %s", d.Remedy)
	}
	return b.String()
}

// Diagnostics is an ordered collection of configuration problems.
type Diagnostics []Diagnostic

func (ds *Diagnostics) errorf(field, message, remedy string) {
	*ds = append(*ds, Diagnostic{Severity: SeverityError, Field: field, Message: message, Remedy: remedy})
}

func (ds *Diagnostics) warnf(field, message, remedy string) {
	*ds = append(*ds, Diagnostic{Severity: SeverityWarning, Field: field, Message: message, Remedy: remedy})
}

// Warn appends a warning. Exported for services outside this package, such as
// workspace enumeration, that produce configuration-shaped diagnostics.
func (ds *Diagnostics) Warn(field, message, remedy string) {
	ds.warnf(field, message, remedy)
}

// Error appends an error.
func (ds *Diagnostics) Error(field, message, remedy string) {
	ds.errorf(field, message, remedy)
}

// HasErrors reports whether any diagnostic blocks startup.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Counts returns the number of errors and warnings.
func (ds Diagnostics) Counts() (errors, warnings int) {
	for _, d := range ds {
		if d.Severity == SeverityError {
			errors++
			continue
		}
		warnings++
	}
	return errors, warnings
}

// Write renders diagnostics for a terminal, errors first.
func (ds Diagnostics) Write(w io.Writer) {
	for _, d := range ds {
		if d.Severity == SeverityError {
			fmt.Fprintln(w, d)
		}
	}
	for _, d := range ds {
		if d.Severity == SeverityWarning {
			fmt.Fprintln(w, d)
		}
	}
}
