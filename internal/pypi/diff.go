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

// lineEdit is one step in a diff edit script.
type lineEdit struct {
	op   byte   // '=' keep, '-' delete from a, '+' insert from b
	line string
	aIdx int // 1-based line number in a; 0 for pure insertions
	bIdx int // 1-based line number in b; 0 for pure deletions
}

// computeEdits builds the shortest edit script for a→b using an LCS
// dynamic-programming table. Deletions are preferred over insertions on ties,
// producing standard -old / +new output for substitutions.
func computeEdits(a, b []string) []lineEdit {
	m, n := len(a), len(b)

	// dp[i][j] = LCS length of a[i:] and b[j:], built bottom-up.
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	edits := make([]lineEdit, 0, m+n)
	i, j := 0, 0
	aLine, bLine := 1, 1
	for i < m || j < n {
		switch {
		case i < m && j < n && a[i] == b[j]:
			edits = append(edits, lineEdit{'=', a[i], aLine, bLine})
			i++
			j++
			aLine++
			bLine++
		case j >= n || (i < m && dp[i+1][j] >= dp[i][j+1]):
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

// buildHunks groups the edit script into unified-diff hunk strings with ctx
// lines of context on each side of every changed region.
func buildHunks(a, b []string, ctx int) []string {
	edits := computeEdits(a, b)

	type span struct{ lo, hi int }
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

	var hunks []string
	for _, sp := range spans {
		slice := edits[sp.lo:sp.hi]

		aStart, bStart, aCount, bCount := 0, 0, 0, 0
		for _, e := range slice {
			switch e.op {
			case '=':
				if aStart == 0 {
					aStart = e.aIdx
				}
				if bStart == 0 {
					bStart = e.bIdx
				}
				aCount++
				bCount++
			case '-':
				if aStart == 0 {
					aStart = e.aIdx
				}
				aCount++
			case '+':
				if bStart == 0 {
					bStart = e.bIdx
				}
				bCount++
			}
		}
		// aStart==0 with aCount==0 is correct for pure-insertion hunks:
		// it produces @@ -0,0 +1,N @@ per the unified-diff spec. The clamp
		// to 1 is only a safety net for the impossible case where a-lines
		// exist but no start line was captured.
		if aStart == 0 && aCount > 0 {
			aStart = 1
		}
		// Symmetric guard for the b side (pure-deletion hunks).
		if bStart == 0 && bCount > 0 {
			bStart = 1
		}

		var hunk strings.Builder
		fmt.Fprintf(&hunk, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, e := range slice {
			switch e.op {
			case '=':
				fmt.Fprintf(&hunk, " %s\n", e.line)
			case '-':
				fmt.Fprintf(&hunk, "-%s\n", e.line)
			case '+':
				fmt.Fprintf(&hunk, "+%s\n", e.line)
			}
		}
		hunks = append(hunks, hunk.String())
	}
	return hunks
}
