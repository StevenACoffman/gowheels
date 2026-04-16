package pypi

import (
	"fmt"
	"strings"
)

const (
	// DiffReadmeLimit is the maximum number of README body bytes shown in the
	// unified diff section. The full README is always printed separately in the
	// local-text block so the user can inspect it without the diff obscuring it.
	DiffReadmeLimit = 512

	// DiffContext is the number of unchanged context lines shown on each side
	// of a changed hunk in unified diff output.
	DiffContext = 3
)

// lineEdit is one step in a diff edit script.
type lineEdit struct {
	op   byte // '=' keep, '-' delete from a, '+' insert from b
	line string
	aIdx int // 1-based line number in a; 0 for pure insertions
	bIdx int // 1-based line number in b; 0 for pure deletions
}

// span is a half-open [lo, hi) index range into an edit slice, used to
// collect the edit regions plus their surrounding context lines.
type span struct{ lo, hi int }

// UnifiedDiff returns a unified diff string comparing aText and bText,
// labelled with fromLabel and toLabel. Returns an empty string when the
// texts are identical.
func UnifiedDiff(fromLabel, toLabel, aText, bText string) string {
	if aText == bText {
		return ""
	}
	aLines := splitLines(aText)
	bLines := splitLines(bText)
	hunks := buildHunks(aLines, bLines, DiffContext)
	if len(hunks) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n", fromLabel)
	fmt.Fprintf(&out, "+++ %s\n", toLabel)
	for _, h := range hunks {
		out.WriteString(h)
	}
	return out.String()
}

// TruncateReadmeBody replaces a long README body in a METADATA text with a
// short placeholder so that unified diffs remain readable. The caller is
// responsible for printing the full text in a separate section.
//
// RFC 822 / PEP 566 METADATA format uses a single blank line ("\n\n") as the
// mandatory separator between the header block and the long-description body.
// Headers never contain blank lines themselves, so the first "\n\n" in the
// text is always and only the header/body separator. If no such separator is
// found the text has no body and is returned unchanged.
func TruncateReadmeBody(text string) string {
	// RFC 822 header/body separator — the first blank line in the text.
	const headerBodySep = "\n\n"
	idx := strings.Index(text, headerBodySep)
	if idx == -1 {
		return text // no body present
	}
	body := text[idx+len(headerBodySep):]
	if len(body) <= DiffReadmeLimit {
		return text
	}
	omitted := len(body) - DiffReadmeLimit
	return text[:idx+len(headerBodySep)] +
		body[:DiffReadmeLimit] +
		fmt.Sprintf("\n[... %d bytes omitted from diff]\n", omitted)
}

// splitLines splits s on "\n", dropping the trailing empty element produced
// when s ends with a newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// buildLCSTable returns a bottom-up LCS length table for a and b.
// dp[i][j] is the length of the longest common subsequence of a[i:] and b[j:].
func buildLCSTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	return dp
}

// computeEdits builds the shortest edit script for a→b using the LCS table
// from buildLCSTable. Deletions are preferred over insertions on ties,
// producing standard -old / +new output for substitutions.
func computeEdits(a, b []string) []lineEdit {
	dp := buildLCSTable(a, b)
	edits := make([]lineEdit, 0, len(a)+len(b))
	i, j := 0, 0
	aLine, bLine := 1, 1
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			edits = append(edits, lineEdit{'=', a[i], aLine, bLine})
			i++
			j++
			aLine++
			bLine++
		case j >= len(b) || (i < len(a) && dp[i+1][j] >= dp[i][j+1]):
			// Prefer deletions on ties so substitutions render as -old / +new.
			edits = append(edits, lineEdit{'-', a[i], aLine, 0})
			i++
			aLine++
		default:
			edits = append(edits, lineEdit{'+', b[j], 0, bLine})
			j++
			bLine++
		}
	}
	return edits
}

// collectSpans identifies the index ranges within the edit slice that cover
// each changed region plus ctx lines of context on each side. Adjacent ranges
// that would overlap (lo <= previous hi) are merged into a single span.
func collectSpans(edits []lineEdit, ctx int) []span {
	var spans []span
	for i, e := range edits {
		if e.op == '=' {
			continue
		}
		lo := max(0, i-ctx)
		hi := min(len(edits), i+ctx+1)
		if len(spans) > 0 && lo <= spans[len(spans)-1].hi {
			spans[len(spans)-1].hi = hi
		} else {
			spans = append(spans, span{lo, hi})
		}
	}
	return spans
}

// firstNonZero returns a when a is non-zero, otherwise b. Used to latch the
// first non-zero line index seen while scanning a hunk's edit slice.
func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// clampStart returns 1 when start is 0 and count is positive, otherwise start.
// aStart==0 with aCount==0 is the correct unified-diff convention for a
// pure-insertion hunk (@@ -0,0 +1,N @@). The clamp to 1 is only a safety net
// for an impossible LCS-trace state.
func clampStart(start, count int) int {
	if start == 0 && count > 0 {
		return 1
	}
	return start
}

// hunkStats computes the a-side and b-side start lines and counts for a hunk
// header (@@ -aStart,aCount +bStart,bCount @@).
//
// aIdx is non-zero for '=' and '-' edits; bIdx is non-zero for '=' and '+'
// edits. Counting op != '+' for aCount and op != '-' for bCount avoids nested
// switch/if logic while remaining equivalent to the per-case accounting.
func hunkStats(slice []lineEdit) (aStart, bStart, aCount, bCount int) {
	for _, e := range slice {
		if e.aIdx != 0 {
			aStart = firstNonZero(aStart, e.aIdx)
		}
		if e.bIdx != 0 {
			bStart = firstNonZero(bStart, e.bIdx)
		}
		if e.op != '+' {
			aCount++
		}
		if e.op != '-' {
			bCount++
		}
	}
	aStart = clampStart(aStart, aCount)
	bStart = clampStart(bStart, bCount)
	return aStart, bStart, aCount, bCount
}

// renderHunk formats a single unified-diff hunk as a string including the
// @@ header and prefixed content lines.
func renderHunk(aStart, bStart, aCount, bCount int, slice []lineEdit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
	for _, e := range slice {
		switch e.op {
		case '=':
			fmt.Fprintf(&b, " %s\n", e.line)
		case '-':
			fmt.Fprintf(&b, "-%s\n", e.line)
		case '+':
			fmt.Fprintf(&b, "+%s\n", e.line)
		}
	}
	return b.String()
}

// buildHunks groups the edit script into unified-diff hunk strings with ctx
// lines of context on each side of every changed region.
func buildHunks(a, b []string, ctx int) []string {
	edits := computeEdits(a, b)
	spans := collectSpans(edits, ctx)
	hunks := make([]string, 0, len(spans))
	for _, sp := range spans {
		slice := edits[sp.lo:sp.hi]
		aStart, bStart, aCount, bCount := hunkStats(slice)
		hunks = append(hunks, renderHunk(aStart, bStart, aCount, bCount, slice))
	}
	return hunks
}
