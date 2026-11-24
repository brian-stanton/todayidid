package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxLocationRunes = 120
	metadataPrefix   = "<!-- todayidid: "
	metadataSuffix   = " -->"
	maxMetadataBytes = 1024
)

type entryMetadata struct {
	Version  int    `json:"version"`
	Location string `json:"location,omitempty"`
}

// NormalizeLocation validates a manually supplied single-line location and
// removes surrounding spaces. It performs no location lookup or inference.
func NormalizeLocation(location string) (string, error) {
	if !utf8.ValidString(location) {
		return "", errors.New("location must contain valid UTF-8 text")
	}
	for _, r := range location {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", errors.New("location must be one line without control characters")
		}
	}
	location = strings.TrimSpace(location)
	if utf8.RuneCountInString(location) > MaxLocationRunes {
		return "", fmt.Errorf("location must contain at most %d characters", MaxLocationRunes)
	}
	return location, nil
}

func encodeEntry(content, location string) (string, error) {
	if location == "" && !strings.HasPrefix(content, metadataPrefix) {
		return content, nil
	}
	// json.Marshal's HTML escaping prevents a location containing "-->" from
	// closing the comment. An empty header escapes a body using our prefix.
	metadata, err := json.Marshal(entryMetadata{Version: 1, Location: location})
	if err != nil {
		return "", err
	}
	header := metadataPrefix + string(metadata) + metadataSuffix + "\n\n"
	if len(header) > maxMetadataBytes {
		return "", errors.New("entry metadata exceeds its size limit")
	}
	return header + content, nil
}

func decodeEntry(raw string) (content, location string, err error) {
	content = raw
	if strings.HasPrefix(raw, metadataPrefix) {
		header, rest, found := strings.Cut(raw, "\n")
		header = strings.TrimSuffix(header, "\r")
		if !found || len(header)+2 > maxMetadataBytes || !strings.HasSuffix(header, metadataSuffix) {
			return "", "", errors.New("malformed todayidid metadata header")
		}
		// Accept either LF or CRLF separators after external text editing.
		switch {
		case strings.HasPrefix(rest, "\r\n"):
			content = rest[2:]
		case strings.HasPrefix(rest, "\n"):
			content = rest[1:]
		default:
			return "", "", errors.New("todayidid metadata must be followed by a blank line")
		}
		payload := strings.TrimSuffix(strings.TrimPrefix(header, metadataPrefix), metadataSuffix)
		decoder := json.NewDecoder(strings.NewReader(payload))
		decoder.DisallowUnknownFields()
		var metadata entryMetadata
		if err := decoder.Decode(&metadata); err != nil {
			return "", "", fmt.Errorf("invalid todayidid metadata: %w", err)
		}
		if decoder.Decode(new(any)) != io.EOF {
			return "", "", errors.New("invalid trailing data in todayidid metadata")
		}
		if metadata.Version != 1 {
			return "", "", fmt.Errorf("unsupported todayidid metadata version %d", metadata.Version)
		}
		location, err = NormalizeLocation(metadata.Location)
		if err != nil {
			return "", "", fmt.Errorf("invalid todayidid metadata: %w", err)
		}
	}
	if len(content) > MaxEntryBytes {
		return "", "", fmt.Errorf("entry exceeds the %d MiB limit", MaxEntryBytes>>20)
	}
	return content, location, nil
}
