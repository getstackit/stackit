package common

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// ReadMessage resolves a commit message from either an inline -m string or a
// --message-file path. Used by create/modify/squash/split to keep variable
// commit-message text out of the literal command line, which keeps Claude
// permission rules stable across distinct messages and avoids shell-escaping
// pitfalls for multi-line subject+body messages.
//
// If messageFile is "-", reads from stdin. If both flags are set, returns an
// error. Trims surrounding whitespace; preserves internal newlines so
// multi-line bodies survive intact. Errors when the resolved message is empty
// (file is empty or stdin received nothing) — the caller passed --message-file
// expecting a message, so silent fallthrough to other input paths would mask
// the mistake.
func ReadMessage(message, messageFile string) (string, error) {
	if message != "" && messageFile != "" {
		return "", errors.New("cannot use --message and --message-file together; pass only one (use --message-file - to read from stdin)")
	}
	if messageFile == "" {
		return message, nil
	}
	return readMessageFrom(messageFile, os.Stdin)
}

func readMessageFrom(path string, stdin *os.File) (string, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		// Refuse to block on a terminal — the user almost certainly meant to
		// pipe content and forgot.
		if stat, statErr := stdin.Stat(); statErr == nil && stat.Mode()&os.ModeCharDevice != 0 {
			return "", errors.New("--message-file - requires piped input; got terminal")
		}
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("--message-file %q: file not found (use \"-\" to read from stdin): %w", path, err)
		}
	}
	if err != nil {
		return "", fmt.Errorf("read --message-file %q: %w", path, err)
	}

	msg := strings.TrimSpace(string(data))
	if msg == "" {
		if path == "-" {
			return "", errors.New("--message-file - received empty input")
		}
		return "", fmt.Errorf("--message-file %q is empty", path)
	}
	return msg, nil
}
