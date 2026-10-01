package cel

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// celErrorRegex extracts position information from CEL compilation errors.
	celErrorRegex = regexp.MustCompile(`ERROR:\s+<input>:(\d+):(\d+):\s+(.+)`)
)

// extractErrorPosition parses CEL error messages to locate the error position.
// Returns (0, 0) if position information is not available.
func extractErrorPosition(err error) (line, column int) {
	if err == nil {
		return 0, 0
	}

	matches := celErrorRegex.FindStringSubmatch(err.Error())
	if len(matches) >= 4 {
		if l, parseErr := strconv.Atoi(matches[1]); parseErr == nil {
			line = l
		}
		if c, parseErr := strconv.Atoi(matches[2]); parseErr == nil {
			column = c
		}
	}
	return line, column
}

// formatFilterError formats a CEL compilation error with helpful context including
// position information, available fields, and documentation links.
// Used internally by CompileFilter to provide user-friendly error messages.
func formatFilterError(err error) string {
	if err == nil {
		return "Invalid filter expression"
	}

	line, column := extractErrorPosition(err)
	errMsg := simplifyErrorMessage(err.Error())

	var msg strings.Builder

	if column > 0 {
		msg.WriteString(fmt.Sprintf("Invalid filter at line %d, column %d: %s", line, column, errMsg))
	} else {
		msg.WriteString(fmt.Sprintf("Invalid filter: %s", errMsg))
	}

	msg.WriteString(". Available fields: auditID, verb, requestReceivedTimestamp, objectRef.namespace, objectRef.resource, objectRef.name, user.username, user.groups, responseStatus.code, location")
	msg.WriteString(". See https://cel.dev for CEL syntax")

	return msg.String()
}

// simplifyErrorMessage strips CEL implementation details from error messages.
// Removes internal prefixes and multi-line context to make errors more readable.
func simplifyErrorMessage(celError string) string {
	msg := celError

	msg = strings.ReplaceAll(msg, "ERROR: <input>:", "")

	if idx := strings.Index(msg, ": "); idx != -1 && idx < 10 {
		parts := strings.SplitN(msg, ": ", 2)
		if len(parts) == 2 {
			msg = parts[1]
		}
	}

	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}

	return strings.TrimSpace(msg)
}
