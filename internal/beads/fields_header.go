package beads

import "strings"

// Attachment fields live in the description's header block (gt-pd1c.75).
//
// SetAttachmentFields writes them as the first lines of the description,
// followed by a blank line and the free text. ParseAttachmentFields and
// SetAttachmentFields read and replace them in the header block only, so a
// "key: value" line in a fenced or indented code block, a block quote, a list
// or a later paragraph never counts as a field and is never rewritten.
//
// The header block is the first paragraph, outside fenced code blocks, whose
// first line is a field line with a known attachment key (attachmentFieldKeys)
// and is not indented as code. Free text above it (e.g. text a person put in
// front of the fields) is tolerated and kept. The block runs to the first
// blank line or the first line that opens a Markdown block (fence ``` or ~~~,
// block quote '>', heading '#', list item, table row '|'), with one exception
// for data written by older gt:
//
// Legacy multi-line values. attached_args (always) and formula_vars (unless
// in the current one-line JSON form) were once written raw, so their value
// could span lines, including blank lines, lists, headings and fences. After
// such a field, every line up to the next line with a known attachment key is
// continuation text and the header continues there; without a later known
// key the normal end applies. Continuation lines are ignored by the parser and
// kept verbatim by SetAttachmentFields. FormatAttachmentFields writes every
// value on one line, so new descriptions never rely on this.

// attachmentFieldKeys maps every accepted spelling of an attachment key
// (lowercase) to its canonical name. It is the one key list: the parser, the
// writer and the review_only helpers all read it.
var attachmentFieldKeys = map[string]string{
	"attached_molecule": "attached_molecule", "attached-molecule": "attached_molecule", "attachedmolecule": "attached_molecule",
	"attached_formula": "attached_formula", "attached-formula": "attached_formula", "attachedformula": "attached_formula",
	"attached_at": "attached_at", "attached-at": "attached_at", "attachedat": "attached_at",
	"attached_args": "attached_args", "attached-args": "attached_args", "attachedargs": "attached_args",
	"attached_vars": "attached_vars", "attached-vars": "attached_vars", "attachedvars": "attached_vars",
	"dispatched_by": "dispatched_by", "dispatched-by": "dispatched_by", "dispatchedby": "dispatched_by",
	"no_merge": "no_merge", "no-merge": "no_merge", "nomerge": "no_merge",
	"review_only": "review_only", "review-only": "review_only", "reviewonly": "review_only",
	"mode":      "mode",
	"convoy_id": "convoy_id", "convoy-id": "convoy_id", "convoyid": "convoy_id", "convoy": "convoy_id",
	"merge_strategy": "merge_strategy", "merge-strategy": "merge_strategy", "mergestrategy": "merge_strategy",
	"convoy_owned": "convoy_owned", "convoy-owned": "convoy_owned", "convoyowned": "convoy_owned",
	"formula_vars": "formula_vars", "formula-vars": "formula_vars", "formulavars": "formula_vars",
}

// canonicalAttachmentKey returns the canonical attachment key for key, or ""
// when key is not an attachment key.
func canonicalAttachmentKey(key string) string {
	return attachmentFieldKeys[strings.ToLower(strings.TrimSpace(key))]
}

func isReviewOnlyKey(key string) bool { return canonicalAttachmentKey(key) == "review_only" }

// attachmentFieldLine parses a field line whose key is a known attachment key
// and returns the canonical key.
func attachmentFieldLine(line string) (key, value string, ok bool) {
	k, v, isField := headerFieldLine(line)
	if !isField {
		return "", "", false
	}
	key = canonicalAttachmentKey(k)
	return key, v, key != ""
}

// splitAttachmentHeader splits description lines into the free text before
// the header block, the header block and the lines after it. Without a header
// block, pre holds every line.
func splitAttachmentHeader(description string) (pre, header, rest []string) {
	lines := strings.Split(description, "\n")
	inFence := false
	paragraphStart := true
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case isFenceLine(trimmed):
			inFence = !inFence
			paragraphStart = false
			continue
		case inFence:
			continue
		case trimmed == "":
			paragraphStart = true
			continue
		}
		if paragraphStart && !isIndentedCode(line) {
			if _, _, ok := attachmentFieldLine(line); ok {
				end := attachmentHeaderEnd(lines, i)
				return lines[:i], lines[i:end], lines[end:]
			}
		}
		paragraphStart = false
	}
	return lines, nil, nil
}

// attachmentHeaderEnd returns the index after the header block starting at
// start (a known attachment field line).
func attachmentHeaderEnd(lines []string, start int) int {
	i := start
	for i < len(lines) {
		if key, value, ok := attachmentFieldLine(lines[i]); ok && hasLegacyContinuation(key, value) {
			if next := nextAttachmentFieldLine(lines, i+1); next > 0 {
				i = next
				continue
			}
		}
		i++
		if i >= len(lines) || endsAttachmentHeader(lines[i]) {
			return i
		}
	}
	return i
}

// hasLegacyContinuation reports whether a field may continue on the next
// lines because older gt wrote its value raw (see the package comment above).
func hasLegacyContinuation(key, value string) bool {
	switch key {
	case "attached_args":
		return true
	case "formula_vars":
		return !strings.HasPrefix(value, "[")
	}
	return false
}

// nextAttachmentFieldLine returns the index of the first line from `from` on
// with a known attachment key, or -1.
func nextAttachmentFieldLine(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		if _, _, ok := attachmentFieldLine(lines[i]); ok {
			return i
		}
	}
	return -1
}

// headerFieldLine parses a "key: value" field line. ok is false when the line
// does not start with an identifier key (letters, digits, '_' and '-',
// starting with a letter) followed by a colon.
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

func isFenceLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// isIndentedCode reports a line indented as a Markdown code block.
func isIndentedCode(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
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
// stays one header line.
func singleLine(value string) string {
	if !strings.ContainsAny(value, "\r\n") {
		return value
	}
	return strings.Join(strings.Fields(value), " ")
}

// review_only values (gt-pd1c.75, R2-6). Only "true" and "false" (any case)
// are canonical. Any other value, the empty value included ("no", "off", "0",
// "maybe", ""), still marks the bead review-only, failing closed, and is kept
// exactly as written by every rewrite: tooling never turns it into a canonical
// "true" (or drops it) on its own. gt sling, gt done and gt mq submit warn
// about it; a person fixes the value or clears the marker with
// gt bead clear-review-only.

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
	// NonCanonical: the marking value is not true/false (and no canonical
	// true line is present). Value holds it as written (may be "").
	NonCanonical bool
	Value        string
	// IgnoredLines counts review_only lines outside the header block (code
	// blocks, quotes, list items, later paragraphs). They do not mark the bead.
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
		status.NonCanonical = fields.ReviewOnly && fields.ReviewOnlyNonCanonical
		if status.NonCanonical {
			status.Value = fields.ReviewOnlyRaw
		}
	}
	pre, _, rest := splitAttachmentHeader(issue.Description)
	for _, line := range append(append([]string(nil), pre...), rest...) {
		if key, _, ok := reviewOnlyLikeLine(line); ok && isReviewOnlyKey(key) {
			status.IgnoredLines++
		}
	}
	return status
}

// reviewOnlyLikeLine reads a field line outside the header, after stripping
// quote and list markers, so a marker in a quote or list is reported too.
func reviewOnlyLikeLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimLeft(strings.TrimSpace(line), "> ")
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
	cleared.ReviewOnlyNonCanonical = false
	cleared.ReviewOnlyRaw = ""
	return writeAttachmentFields(issue, &cleared)
}

// StripAttachmentFields returns description without its attachment header
// fields: the free text, verbatim (e.g. for a PR body).
func StripAttachmentFields(description string) string {
	return writeAttachmentFields(&Issue{Description: description}, nil)
}
