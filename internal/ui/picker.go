package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// PickerOption is one gateway the launch picker offers.
type PickerOption struct {
	Name string
	// URL is the endpoint, or a summary of a provider switch.
	URL string
	// Store and Key name the secret; empty for a keyless gateway.
	Store, Key string
	// Warning is shown after the line, such as the cloud access a
	// provider switch opens.
	Warning string
}

// PickerNone is the value the picker returns for the harness's own
// login.
const PickerNone = "none"

// Remember is how long a picked gateway stays chosen.
type Remember string

// Remember scopes, in the order the picker offers them.
const (
	RememberOnce    Remember = "once"
	RememberRepo    Remember = "repo"
	RememberProfile Remember = "profile"
)

// ErrPickerAborted reports an answer stream that ended before a choice
// was made.
var ErrPickerAborted = errors.New("gateway choice aborted")

// PickGateway asks which of options harness should use, or none for
// its own login, then how long to remember the answer. Nothing is
// preselected: an empty answer to the first question asks again. The
// repo scope is offered only when canRepo. It reads from in and writes
// to out, and never decrypts anything.
func PickGateway(in *bufio.Reader, out io.Writer, harness, profile string, options []PickerOption, canRepo bool) (string, Remember, error) {
	fmt.Fprintf(out, "aide: %s has %d gateways marked in profile %q and none chosen:\n\n", harness, len(options), profile)
	for i, o := range options {
		secret := "no key"
		if o.Store != "" {
			secret = o.Store + "/" + o.Key
		}
		fmt.Fprintf(out, "  %d) %s  %s  (%s)\n", i+1, o.Name, o.URL, secret)
		if o.Warning != "" {
			fmt.Fprintf(out, "       warning: %s\n", o.Warning)
		}
	}
	fmt.Fprintf(out, "  %d) none  use %s's own login\n\n", len(options)+1, harness)
	n, err := ask(in, out, fmt.Sprintf("Gateway for %s [1-%d]: ", harness, len(options)+1), len(options)+1, false)
	if err != nil {
		return "", "", err
	}
	choice := PickerNone
	if n <= len(options) {
		choice = options[n-1].Name
	}
	type scope struct {
		r     Remember
		label string
	}
	scopes := []scope{{RememberOnce, "Just this once"}}
	if canRepo {
		scopes = append(scopes, scope{RememberRepo, "Always in this repo"})
	}
	scopes = append(scopes, scope{RememberProfile, "Always in profile " + profile})
	fmt.Fprintln(out)
	for i, s := range scopes {
		fmt.Fprintf(out, "  %d) %s\n", i+1, s.label)
	}
	n, err = ask(in, out, fmt.Sprintf("Remember [1-%d, Enter for once]: ", len(scopes)), len(scopes), true)
	if err != nil {
		return "", "", err
	}
	return choice, scopes[n-1].r, nil
}

// ask reads a number in 1..limit, asking again on anything else. With
// emptyIsFirst an empty line answers 1.
func ask(in *bufio.Reader, out io.Writer, prompt string, limit int, emptyIsFirst bool) (int, error) {
	for {
		fmt.Fprint(out, prompt)
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" && err != nil {
			return 0, ErrPickerAborted
		}
		if line == "" && emptyIsFirst {
			return 1, nil
		}
		if n, convErr := strconv.Atoi(line); convErr == nil && n >= 1 && n <= limit {
			return n, nil
		}
		if err != nil {
			return 0, ErrPickerAborted
		}
		fmt.Fprintf(out, "Enter a number from 1 to %d.\n", limit)
	}
}
