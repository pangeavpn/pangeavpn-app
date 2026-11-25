package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// ErrLogTooLarge bounds what a caller may ask to read from the file log.
var ErrLogTooLarge = errors.New("log read too large")

// TailEntries returns the last maxBytes of a JSON-lines log, dropping a first
// entry the cut ran through rather than reporting it half-parsed.
func TailEntries(path string, maxBytes int64) ([]LogEntry, error) {
	data, err := tailBytes(path, maxBytes)
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(data, []byte("\n"))
	entries := make([]LogEntry, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// TailText returns the last maxBytes of a text log. A cut through a line is
// left to the reader: panic traces are worth more than a clean first line.
func TailText(path string, maxBytes int64) (string, error) {
	data, err := tailBytes(path, maxBytes)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func tailBytes(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > 1<<20 {
		return nil, ErrLogTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	length := min(info.Size(), maxBytes)
	buffer := make([]byte, length)
	if _, err := file.ReadAt(buffer, info.Size()-length); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buffer, nil
}
