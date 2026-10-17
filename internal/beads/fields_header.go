package beads

import "strings"

// Attachment fields live in the description's header block (gt-pd1c.75).
//
// SetAttachmentFields writes them as the first lines of the description,
// followed by a blank line and the free text. ParseAttachmentFields and
// SetAttachmentFields read and replace them in the header block only, so a
// "key: value" line in a fenced or indented code block, a block quote, a list
// or a later paragraph is not read as a field and is never rewritten. The one
// exception is review_only, which fails closed (below).
//
// Header block. The first paragraph, outside fenced code blocks and not
// indented as code, whose first line is a field with a strong attachment key:
// any attachment key except the weak spellings "mode" and "convoy", which are
// ordinary words that an opening prose line ("Mode: fast please") may start
// with. Weak keys are still read inside the header. Free text above the header
// is tolerated and kept. The block runs to the first blank line or the first
// line that opens a Markdown block (fence ``` or ~~~, block quote '>', heading
// '#', list item, table row '|'). Within it the first occurrence of a key wins;
// review_only lines are OR-ed and formula_vars lines are joined.
//
// Legacy multi-line values. Older gt wrote attached_args and formula_vars raw,
// so their value could span lines (blank lines, lists, headings and fences
// included). FormatAttachmentFields now writes attached_args as a JSON string
// and formula_vars as a JSON array, always on one line; only a non-JSON value
// is legacy. After a legacy field, the header continues at the first later
// line that (1) has a key the old writer put after that field
// (attachmentKeyOrder) and (2) starts a run of field lines up to a blank line
// or the end. The lines skipped are continuation text: ignored by the parser,
// kept verbatim by SetAttachmentFields. Since formula_vars was always written
// last, only attached_args can continue onto later fields.
//
// review_only fails closed. Besides the header, a review_only line counts when
// it is in a later field paragraph (a paragraph outside fences whose first line
// has any attachment key, weak or strong) or in a further run that a legacy
// field could continue onto. Other keys there are not read. Such lines are
// consolidated into the header by every rewrite and removed by
// WithoutReviewOnlyMarker. Code blocks, quotes and list items never count.

// attachmentFieldKeys maps every accepted spelling of an attachment key
// (lowercase) to its canonical name. It is the one key list: the parser, the
// writer, the review_only helpers and readiness's machine keys all read it.
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

// weakAttachmentKeys are spellings that do not start a header block.
var weakAttachmentKeys = map[string]bool{"mode": true, "convoy": true}

// attachmentKeyOrder is the order in which FormatAttachmentFields writes the
// fields (unchanged since before gt-pd1c.75); legacy continuation only moves
// forward in it.
var attachmentKeyOrder = map[string]int{
	"attached_molecule": 0, "attached_formula": 1, "attached_at": 2, "attached_args": 3,
	"attached_vars": 4, "dispatched_by": 5, "no_merge": 6, "review_only": 7, "mode": 8,
	"convoy_id": 9, "merge_strategy": 10, "convoy_owned": 11, "formula_vars": 12,
}

// canonicalAttachmentKey returns the canonical attachment key for key, or ""
// when key is not an attachment key.
func canonicalAttachmentKey(key string) string {
	return attachmentFieldKeys[strings.ToLower(strings.TrimSpace(key))]
}

func isReviewOnlyKey(key string) bool { return canonicalAttachmentKey(key) == "review_only" }

// isStrongAttachmentKey reports a key spelling that may start a header block.
func isStrongAttachmentKey(key string) bool {
	return canonicalAttachmentKey(key) != "" && !weakAttachmentKeys[strings.ToLower(strings.TrimSpace(key))]
}

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

// descriptionLayout is where the attachment fields of a description are.
type descriptionLayout struct {
	lines []string
	// headerStart/headerEnd delimit the header block; headerStart is -1
	// without one.
	headerStart, headerEnd int
	// extraReviewOnly marks review_only lines outside the header that count
	// (fail closed).
	extraReviewOnly map[int]bool
}

func (l descriptionLayout) header() []string {
	if l.headerStart < 0 {
		return nil
	}
	return l.lines[l.headerStart:l.headerEnd]
}

// outside returns the lines before and after the header, without the extra
// review_only lines.
func (l descriptionLayout) outside() (pre, rest []string) {
	keep := func(from, to int) []string {
		var out []string
		for i := from; i < to; i++ {
			if !l.extraReviewOnly[i] {
				out = append(out, l.lines[i])
			}
		}
		return out
	}
	if l.headerStart < 0 {
		return keep(0, len(l.lines)), nil
	}
	return keep(0, l.headerStart), keep(l.headerEnd, len(l.lines))
}

// scanDescription finds the header block and the extra review_only lines.
func scanDescription(description string) descriptionLayout {
	lines := strings.Split(description, "\n")
	layout := descriptionLayout{lines: lines, headerStart: -1, headerEnd: -1, extraReviewOnly: map[int]bool{}}
	inFence := false
	paragraphStart := true
	for i := 0; i < len(lines); i++ {
		line := lines[i]
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
		if !paragraphStart || isIndentedCode(line) {
			paragraphStart = false
			continue
		}
		paragraphStart = false
		rawKey, _, isField := headerFieldLine(line)
		if !isField || canonicalAttachmentKey(rawKey) == "" {
			continue
		}
		if layout.headerStart < 0 && isStrongAttachmentKey(rawKey) {
			end := layout.scanHeader(i)
			layout.headerStart, layout.headerEnd = i, end
			i = end - 1
			continue
		}
		// A later field paragraph: only its review_only lines count.
		end := fieldParagraphEnd(lines, i)
		layout.markReviewOnly(i, end)
		i = end - 1
	}
	return layout
}

// scanHeader returns the end of the header block starting at start, and
// marks the review_only lines of further legacy continuation runs.
func (l descriptionLayout) scanHeader(start int) int {
	lines := l.lines
	i := start
	for i < len(lines) {
		if key, value, ok := attachmentFieldLine(lines[i]); ok && hasLegacyContinuation(key, value) {
			runs := legacyContinuationRuns(lines, i+1, attachmentKeyOrder[key])
			if len(runs) > 0 {
				for _, run := range runs[1:] {
					l.markReviewOnly(run[0], run[1])
				}
				i = runs[0][0]
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

func (l descriptionLayout) markReviewOnly(from, to int) {
	for k := from; k < to; k++ {
		if key, _, ok := attachmentFieldLine(l.lines[k]); ok && key == "review_only" {
			l.extraReviewOnly[k] = true
		}
	}
}

// fieldParagraphEnd returns the index after the paragraph starting at start.
func fieldParagraphEnd(lines []string, start int) int {
	end := start + 1
	for end < len(lines) && !endsAttachmentHeader(lines[end]) {
		end++
	}
	return end
}

// legacyContinuationRuns returns the [start, end) runs, from `from` on, that a
// legacy field of order minOrder may continue onto: a first line with a key
// written after it, and only field lines up to a blank line or the end.
func legacyContinuationRuns(lines []string, from, minOrder int) [][2]int {
	var runs [][2]int
	for k := from; k < len(lines); k++ {
		key, _, ok := attachmentFieldLine(lines[k])
		if !ok || attachmentKeyOrder[key] <= minOrder {
			continue
		}
		end := k
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			if _, _, isField := attachmentFieldLine(lines[end]); !isField {
				break
			}
			end++
		}
		if end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			continue // the run is not keys up to a blank line or the end
		}
		runs = append(runs, [2]int{k, end})
		k = end
	}
	return runs
}

// hasLegacyContinuation reports whether a field value was written raw by older
// gt and may continue on the next lines (see above).
func hasLegacyContinuation(key, value string) bool {
	switch key {
	case "attached_args":
		_, isJSON := decodeAttachedArgs(value)
		return !isJSON
	case "formula_vars":
		return !strings.HasPrefix(value, "[")
	}
	return false
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
	// IgnoredLines counts review_only lines that do not count: in code
	// blocks, quotes, list items or prose paragraphs. They do not mark the bead.
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
	pre, rest := scanDescription(issue.Description).outside()
	for _, line := range append(pre, rest...) {
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
