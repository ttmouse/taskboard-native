package executor

import "strings"

// Errors can be on stdout while unrelated extension warnings occupy stderr.
// Keep both streams, including their first cause and final context.
func failureDiagnostic(stdout, stderr string) string {
	stdout, stderr = strings.TrimSpace(stdout), strings.TrimSpace(stderr)
	if stdout == "" {
		return diagnosticExcerpt(stderr, outputTail)
	}
	if stderr == "" {
		return diagnosticExcerpt(stdout, outputTail)
	}
	return "stderr：\n" + diagnosticExcerpt(stderr, outputTail/2) +
		"\n\nstdout：\n" + diagnosticExcerpt(stdout, outputTail/2)
}

func diagnosticExcerpt(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit/2]) + "\n…\n" + string(runes[len(runes)-(limit-limit/2):])
}
