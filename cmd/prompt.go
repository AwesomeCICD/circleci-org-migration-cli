package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// isInteractiveTTY returns true when os.Stdin is connected to an interactive
// terminal (character device).  It uses only stdlib syscall wrappers so that
// no third-party TTY library is needed.
func isInteractiveTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Prompter holds the I/O streams used by all interactive prompt functions.
// Prompt text goes to out (stderr by default) so that stdout stays reserved
// for machine-parseable command output.  Answers are read from in (os.Stdin
// by default).  A single buffered reader wraps in so that successive
// readLine calls each consume exactly one line without draining the
// underlying reader.  Injecting synthetic streams in tests drives the whole
// flow without a real TTY.
type Prompter struct {
	r   *bufio.Reader
	out io.Writer
}

// NewPrompter returns a Prompter that writes prompts to out and reads answers
// from in.  Callers should pass cmd.ErrOrStderr() for out and os.Stdin for in.
func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{r: bufio.NewReader(in), out: out}
}

// readLine reads one line from p.r, trims whitespace, and returns it.
// It returns ("", io.EOF) when the reader is exhausted.
func (p *Prompter) readLine() (string, error) {
	line, err := p.r.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	line = strings.TrimSpace(line)
	if err == io.EOF && line != "" {
		// Last line with no trailing newline — still valid.
		return line, nil
	}
	return line, err
}

// ask prints prompt to p.out and returns the trimmed line read from p.in.
func (p *Prompter) ask(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	return p.readLine()
}

// askWithDefault prints label [defaultVal]: and returns the trimmed answer,
// substituting defaultVal when the user enters nothing.
func (p *Prompter) askWithDefault(label, defaultVal string) (string, error) {
	var prompt string
	if defaultVal != "" {
		prompt = fmt.Sprintf("%s [%s]: ", label, defaultVal)
	} else {
		prompt = fmt.Sprintf("%s: ", label)
	}
	ans, err := p.ask(prompt)
	if err != nil {
		return "", err
	}
	if ans == "" {
		return defaultVal, nil
	}
	return ans, nil
}

// askRequired repeatedly prompts until the user enters a non-empty value.
// hint is shown in parentheses after the label when non-empty.
func (p *Prompter) askRequired(label, hint string) (string, error) {
	for {
		var prompt string
		if hint != "" {
			prompt = fmt.Sprintf("%s (%s): ", label, hint)
		} else {
			prompt = fmt.Sprintf("%s: ", label)
		}
		ans, err := p.ask(prompt)
		if err != nil {
			return "", err
		}
		if ans != "" {
			return ans, nil
		}
		fmt.Fprintf(p.out, "  (required — please enter a value)\n")
	}
}

// askSecret reads a secret value (e.g. an API token).  When stdin is an
// interactive terminal, echo is suppressed via term.ReadPassword so typed
// characters are not visible on screen; a trailing newline is printed
// afterward because the user's Enter key is not echoed.  When stdin is a
// pipe or redirected file, input is read as a plain line (no masking is
// possible in that case and no masking is claimed).
func (p *Prompter) askSecret(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprintf(p.out, "%s (input hidden): ", label)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(p.out) // restore cursor to new line after silent Enter
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	// Non-TTY path: read a plain line; do not claim masking.
	fmt.Fprintf(p.out, "%s: ", label)
	return p.readLine()
}

// askSecretRequired repeatedly prompts until the user enters a non-empty
// secret value.
func (p *Prompter) askSecretRequired(label string) (string, error) {
	for {
		val, err := p.askSecret(label)
		if err != nil {
			return "", err
		}
		if val != "" {
			return val, nil
		}
		fmt.Fprintf(p.out, "  (required — please enter a value)\n")
	}
}

// askBool prompts for a yes/no answer.  defaultYes controls the default when
// the user presses enter.  It reprompts on invalid input.
func (p *Prompter) askBool(label string, defaultYes bool) (bool, error) {
	var opts string
	if defaultYes {
		opts = "[Y/n]"
	} else {
		opts = "[y/N]"
	}
	for {
		ans, err := p.ask(fmt.Sprintf("%s %s: ", label, opts))
		if err != nil {
			return false, err
		}
		switch strings.ToLower(ans) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintf(p.out, "  Please enter y or n.\n")
		}
	}
}

// askChoice prompts the user to choose from a numbered list of options.
// It returns the chosen item string.  The first option is the default.
// It reprompts on invalid input.
func (p *Prompter) askChoice(label string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("askChoice: no options provided")
	}
	fmt.Fprintf(p.out, "%s\n", label)
	for i, opt := range options {
		if i == 0 {
			fmt.Fprintf(p.out, "  %d) %s (default)\n", i+1, opt)
		} else {
			fmt.Fprintf(p.out, "  %d) %s\n", i+1, opt)
		}
	}
	for {
		ans, err := p.ask(fmt.Sprintf("Choose [1-%d, default 1]: ", len(options)))
		if err != nil {
			return "", err
		}
		if ans == "" {
			return options[0], nil
		}
		// Try matching a number.
		var n int
		if _, parseErr := fmt.Sscanf(ans, "%d", &n); parseErr == nil {
			if n >= 1 && n <= len(options) {
				return options[n-1], nil
			}
		}
		// Try matching the option text directly.
		for _, opt := range options {
			if strings.EqualFold(ans, opt) {
				return opt, nil
			}
		}
		fmt.Fprintf(p.out, "  Please enter a number between 1 and %d.\n", len(options))
	}
}

// askMultiSelect prompts the user to select zero or more items from a list,
// defaulting to all selected.  The user enters comma-separated numbers, or
// "all" / empty to select all, or "none" to select none.
// It returns the subset of options that were selected.
func (p *Prompter) askMultiSelect(label string, options []string) ([]string, error) {
	return p.askMultiSelectWithDefault(label, options, options)
}

// askMultiSelectWithDefault is like askMultiSelect but lets the caller specify
// which items are pre-selected as the default (empty input selects
// defaultSelected rather than all options).  Pass options itself to get the
// same behaviour as askMultiSelect.
func (p *Prompter) askMultiSelectWithDefault(label string, options []string, defaultSelected []string) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}
	fmt.Fprintf(p.out, "%s\n", label)
	// Build a set of default-selected indices for display.
	defaultSet := make(map[string]bool, len(defaultSelected))
	for _, d := range defaultSelected {
		defaultSet[d] = true
	}
	for i, opt := range options {
		if defaultSet[opt] {
			fmt.Fprintf(p.out, "  %d) %s [default]\n", i+1, opt)
		} else {
			fmt.Fprintf(p.out, "  %d) %s\n", i+1, opt)
		}
	}
	defaultHint := "default"
	if len(defaultSelected) == len(options) {
		defaultHint = "all"
	} else if len(defaultSelected) == 0 {
		defaultHint = "none"
	}
	for {
		ans, err := p.ask(fmt.Sprintf("Select (comma-separated numbers, 'all', or 'none') [%s]: ", defaultHint))
		if err != nil {
			return nil, err
		}
		ans = strings.TrimSpace(ans)
		if ans == "" {
			out := make([]string, len(defaultSelected))
			copy(out, defaultSelected)
			return out, nil
		}
		if strings.EqualFold(ans, "all") {
			out := make([]string, len(options))
			copy(out, options)
			return out, nil
		}
		if strings.EqualFold(ans, "none") {
			return []string{}, nil
		}
		// Parse comma-separated numbers.
		parts := strings.Split(ans, ",")
		var selected []string
		valid := true
		for _, part := range parts {
			part = strings.TrimSpace(part)
			var n int
			if _, parseErr := fmt.Sscanf(part, "%d", &n); parseErr != nil || n < 1 || n > len(options) {
				fmt.Fprintf(p.out, "  Invalid selection %q — enter numbers 1-%d, 'all', or 'none'.\n",
					part, len(options))
				valid = false
				break
			}
			selected = append(selected, options[n-1])
		}
		if valid {
			return selected, nil
		}
	}
}
