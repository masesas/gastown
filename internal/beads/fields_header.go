package beads

import "strings"

// Attachment fields live in the description's header block (gt-pd1c.75).
//
// SetAttachmentFields writes them as the first lines of the description,
// followed by a blank line and the free text. ParseAttachmentFields and
// SetAttachmentFields read and replace them there only, so a "key: value" line
// in the free text, a fenced code block, a block quote or a list never counts
// as a field and is never rewritten.
//
// The header block is:
//   - empty unless the first non-blank line is a field line ("key: value" with
//     an identifier key: letters, digits, '_' and '-', starting with a letter);
//   - otherwise every following non-blank line up to the first blank line or
//     the first line that opens a Markdown block (fence ``` or ~~~, block
//     quote '>', heading '#', list item, table row '|').
//
// Non-field lines inside the header are continuation text (legacy multi-line
// attached_args or formula_vars values written before values were folded to
// one line); they are ignored by the parser and kept by SetAttachmentFields.

// splitAttachmentHeader returns the header block lines and the lines after it.
// Leading blank lines before the header are dropped.
func splitAttachmentHeader(description string) (header, rest []string) {
	lines := strings.Split(description, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start == len(lines) {
		return nil, nil
	}
	if _, _, ok := headerFieldLine(lines[start]); !ok {
		return nil, lines[start:]
	}
	end := start + 1
	for end < len(lines) && !endsAttachmentHeader(lines[end]) {
		end++
	}
	return lines[start:end], lines[end:]
}

// headerFieldLine parses a "key: value" field line. ok is false when the line
// does not start with an identifier key followed by a colon.
func headerFieldLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	colon := strings.Index(trimmed, ":")
	if colon <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:colon])
	if !isFieldKey(key) {
		return "", "", false
	}
	return key, strings.TrimSpace(trimmed[colon+1:]), true
}

func isFieldKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

// endsAttachmentHeader reports whether line closes the header block: a blank
// line or the start of a Markdown block.
func endsAttachmentHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	for _, prefix := range []string{"```", "~~~", ">", "#", "|", "- ", "* ", "+ "} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return isOrderedListItem(trimmed)
}

func isOrderedListItem(trimmed string) bool {
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits+1 >= len(trimmed) {
		return false
	}
	return (trimmed[digits] == '.' || trimmed[digits] == ')') && trimmed[digits+1] == ' '
}

// singleLine folds line breaks in a field value into spaces so every field
// stays one header line: a multi-line value must not end the header block
// before the fields written after it (review_only included).
func singleLine(value string) string {
	if !strings.ContainsAny(value, "\r\n") {
		return value
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

// review_only values (gt-pd1c.75, R2-6). Only "true" and "false" (any case)
// are canonical. Any other value ("no", "off", "0", "maybe", ...) still marks
// the bead review-only, failing closed, and is kept exactly as written by
// every rewrite: tooling never turns it into a canonical "true" (or drops it)
// on its own. gt sling warns about it; a person fixes the value or clears the
// marker with gt bead clear-review-only.

func isReviewOnlyKey(key string) bool {
	switch strings.ToLower(key) {
	case "review_only", "review-only", "reviewonly":
		return true
	}
	return false
}

// classifyReviewOnly returns whether a review_only value marks the bead and
// whether it is canonical.
func classifyReviewOnly(value string) (marked, canonical bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return true, false
}

// ReviewOnlyStatus describes the review_only field of a description.
type ReviewOnlyStatus struct {
	// Marked: the header carries review_only with a canonical true or a
	// non-canonical value (fail closed). Same answer as IsReviewOnly.
	Marked bool
	// NonCanonical is the raw non-canonical value, "" when the marker is
	// absent, canonical, or a canonical true line is present as well.
	NonCanonical string
	// IgnoredLines counts review_only lines outside the header block (free
	// text, code blocks, quotes). They do not mark the bead.
	IgnoredLines int
}

// ReviewOnlyStatusOf reports the review_only status of issue, for callers that
// warn about a non-canonical value or an ignored marker line.
func ReviewOnlyStatusOf(issue *Issue) ReviewOnlyStatus {
	var status ReviewOnlyStatus
	if issue == nil || issue.Description == "" {
		return status
	}
	if fields := ParseAttachmentFields(issue); fields != nil {
		status.Marked = fields.ReviewOnly
		status.NonCanonical = fields.ReviewOnlyRaw
	}
	_, rest := splitAttachmentHeader(issue.Description)
	for _, line := range rest {
		if key, _, ok := reviewOnlyLikeLine(line); ok && isReviewOnlyKey(key) {
			status.IgnoredLines++
		}
	}
	return status
}

// reviewOnlyLikeLine reads a field line outside the header, after stripping
// quote and list markers, so a marker in a quote or list is reported too.
func reviewOnlyLikeLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimLeft(trimmed, "> ")
	for _, prefix := range []string{"- ", "* ", "+ "} {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}
	return headerFieldLine(trimmed)
}

// WithoutReviewOnlyMarker returns description with the review_only field
// removed from its header block; every other field and the free text are
// kept. It is the one way tooling drops the sticky marker (SetAttachmentFields
// never does): gt bead clear-review-only and the rollback of a failed
// gt sling --review-only on a bead that was not review-only before
// (gt-pd1c.75). A description that is not review-only is returned unchanged.
func WithoutReviewOnlyMarker(description string) string {
	issue := &Issue{Description: description}
	fields := ParseAttachmentFields(issue)
	if fields == nil || !fields.ReviewOnly {
		return description
	}
	cleared := *fields
	cleared.ReviewOnly = false
	cleared.ReviewOnlyRaw = ""
	return writeAttachmentFields(issue, &cleared)
}
