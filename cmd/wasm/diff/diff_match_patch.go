package diff

import (
	"strings"
	"time"
)

type DiffMatchPatch struct {
	// Defaults.
	// Set these on your diff_match_patch instance to override the defaults.

	// Number of seconds to map a diff before giving up (0 for infinity).
	Diff_Timeout time.Duration
	// Cost of an empty edit operation in terms of edit characters.
	Diff_EditCost uint16
	// At what point is no match declared (0.0 = perfection, 1.0 = very loose).
	Match_Threshold float32
	// How far to search for a match (0 = exact location, 1000+ = broad match).
	// A match this many characters away from the expected location will add
	// 1.0 to the score (0.0 is a perfect match).
	Match_Distance int32
	// When deleting a large block of text (over ~64 characters), how close does
	// the contents have to match the expected contents. (0.0 = perfection,
	// 1.0 = very loose).  Note that Match_Threshold controls how closely the
	// end points of a delete need to match.
	Patch_DeleteThreshold float32
	// Chunk size for context length.
	Patch_Margin uint16

	// The number of bits in an int.
	Match_MaxBits uint16
}

func New() *DiffMatchPatch {
	// Defaults.
	return &DiffMatchPatch{
		Diff_Timeout:          time.Second,
		Diff_EditCost:         4,
		Match_Threshold:       0.5,
		Match_Distance:        1000,
		Patch_DeleteThreshold: 0.5,
		Patch_Margin:          4,
		Match_MaxBits:         32,
	}
}

// The default diff entry method, sets checklines to true and continues
func (dmp *DiffMatchPatch) diffRecurse(inputA, inputB string) (error, []Diff) {
	return dmp.diffMain(inputA, inputB, true)
}

// Recursive diff method setting a deadline
func (dmp *DiffMatchPatch) diffMain(inputA, inputB string, checklines bool) (error, []Diff) {
	var deadline time.Time

	if dmp.Diff_Timeout > 0 {
		deadline = time.Now().Add(dmp.Diff_Timeout)
	}

	return dmp.diffMainDeadline(inputA, inputB, checklines, deadline)
}

// Diff method with deadline
func (dmp *DiffMatchPatch) diffMainDeadline(inputA, inputB string, checklines bool, deadline time.Time) (error, []Diff) {
	// Check for equality (speedup).
	if inputA == inputB {
		var diffs []Diff
		if inputA != "" {
			diffs = append(diffs, Diff{EQUAL, inputA})
		}
		return nil, diffs
	}

	// Trim off common prefix (speedup).
	commonLength := dmp.diff_commonPrefix(inputA, inputB)
	commonPrefix := inputA[:commonLength]
	textChoppedA := inputA[commonLength:]
	textChoppedB := inputB[commonLength:]

	// Trim off common suffix (speedup).
	commonLength = dmp.diff_commonSuffix(textChoppedA, textChoppedB)
	commonSuffix := textChoppedA[len(inputA)-commonLength:]
	textChoppedA = textChoppedA[:len(inputA)-commonLength]
	textChoppedB = textChoppedB[:len(inputB)-commonLength]

	// Compute the diff on the middle block.
	diffs := dmp.diffCompute(textChoppedA, textChoppedB, checklines, deadline)

	// Restore the prefix and suffix.
	if commonPrefix != "" {
		diffs = append([]Diff{{EQUAL, commonPrefix}}, diffs...)
	}
	if commonSuffix != "" {
		diffs = append(diffs, Diff{EQUAL, commonSuffix})
	}
	diffs = dmp.diff_cleanupMerge(diffs)

	return nil, diffs
}

// * diff_compute_
func (dmp *DiffMatchPatch) diffCompute(textA, textB string, checklines bool, deadline time.Time) []Diff {
	diffs := []Diff{}

	if textA == "" {
		// Just add some text (speedup).
		diffs = append(diffs, Diff{INSERT, textB})
		return diffs
	}

	if textB == "" {
		// Just delete some text (speedup).
		diffs = append(diffs, Diff{DELETE, textA})
		return diffs
	}

	var longtext, shorttext string
	if len(textA) > len(textB) {
		longtext = textA
		shorttext = textB
	} else {
		longtext = textB
		shorttext = textA
	}
	foundTextIndex := strings.Index(longtext, shorttext)
	if foundTextIndex != -1 {
		// Shorter text is inside the longer text (speedup).
		var op Operation

		if len(textA) > len(textB) {
			op = DELETE
		} else {
			op = INSERT
		}
		diffs = append(diffs, Diff{op, longtext[:foundTextIndex]})
		diffs = append(diffs, Diff{EQUAL, shorttext})
		diffs = append(diffs, Diff{op, longtext[foundTextIndex+len(shorttext):]})
		return diffs
	}

	if len(shorttext) == 1 {
		// Single character string.
		// After the previous speedup, the character can't be an equality.
		diffs = append(diffs, Diff{DELETE, textA})
		diffs = append(diffs, Diff{INSERT, textB})
		return diffs
	}

	// Check to see if the problem can be split in two.
	textA_1, textA_2, textB_1, textB_2, midCommon := dmp.diff_halfMatch(textA, textB)

	if textA_1 != "" {
		// A half-match was found.
		// Send both pairs off for separate processing.
		_, diffs_a := dmp.diffMainDeadline(textA_1, textB_1, checklines, deadline)
		_, diffs_b := dmp.diffMainDeadline(textA_2, textB_2, checklines, deadline)

		// Merge the results.
		diffs = append(diffs_a, Diff{EQUAL, midCommon})
		diffs = append(diffs, diffs_b...)
		return diffs
	}

	// Perform a real diff.
	if checklines && len(textA) > 100 && len(textB) > 100 {
		return diff_lineMode(textA, textB, deadline)
	}

	return dmp.diff_bisect_(textA, textB, deadline)
}

// * diff_lineMode_
func (dmp *DiffMatchPatch) diff_lineMode(textA, textB string, deadline time.Time) []Diff {
	// Scan the text on a line-by-line basis first.
	textA, textB, lineArray := dmp.diff_linesToChars(textA, textB)

	_, diffs := dmp.diffMainDeadline(textA, textB, false, deadline)

	// Convert the diff back to original text.
	diffs = dmp.diff_charsToLines(diffs, lineArray)
	// Eliminate freak matches (e.g. blank lines)
	diffs = dmp.DiffCleanupSemantic(diffs)

	// Rediff any replacement blocks, this time character-by-character.
	// Add a dummy entry at the end.
	diffs = append(diffs, Diff{EQUAL, ""})
	count_delete, count_insert := 0, 0
	text_delete := ""
	text_insert := ""

	pointer := 0
	for pointer < len(diffs) {
		switch diffs[pointer].Type {
		case INSERT:
			count_insert++
			text_insert += diffs[pointer].Text
		case DELETE:
			count_delete++
			text_delete += diffs[pointer].Text
		case EQUAL:
			// Upon reaching an equality, check for prior redundancies.
			if count_delete >= 1 && count_insert >= 1 {
				// Delete the offending records and add the merged ones.
				diffs = diffs[:pointer-count_delete-count_insert]
				pointer = pointer - count_delete - count_insert
				_, newDiffs := dmp.diffMainDeadline(text_delete, text_insert, false, deadline)
				for _, newDiff := range newDiffs {
					diffs = append(diffs, newDiff)
					pointer++
				}
				count_insert = 0
				count_delete = 0
				text_delete = ""
				text_insert = ""
			}
		}
	}
	diffs = diffs[:len(diffs)-1]
	return diffs
}

// * diff_bisect_
func (dmp *DiffMatchPatch) diff_bisect_(textA, textB string, deadline time.Time) []Diff {
	textALen := len(textA)
	textBLen := len(textB)
	var max_d int = (textALen + textBLen + 1) / 2
	v_offset := max_d
	v_length := 2 * max_d
	v1 := make([]int, v_length)
	v2 := make([]int, v_length)

	for x := range v1 {
		v1[x] = -1
		v2[x] = -1
	}

	delta := textALen - textBLen

	// If the total number of characters is odd, then the front path will
	// collide with the reverse path.
	front := (delta%2 != 0)

	// Offsets for start and end of k loop.
	// Prevents mapping of space beyond the grid.
	k1start := 0
	k1end := 0
	k2start := 0
	k2end := 0

	for d := 0; d < max_d; d++ {
		// Bail out if deadline is reached.
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}
		// Walk the front path one step.
		for k1 := -d + k1start; k1 <= d-k1end; k1 += 2 {
			k1_offset := v_offset + k1
			var x1 int
			if k1 == -d || (k1 != d && v1[k1_offset-1] < v1[k1_offset+1]) {
				x1 = v1[k1_offset+1]
			} else {
				x1 = v1[k1_offset-1] + 1
			}
			y1 := x1 - k1
			for x1 < textALen && y1 < textBLen && textA[x1] == textB[y1] {
				x1++
				y1++
			}
			v1[k1_offset] = x1
			if x1 > textALen {
				// Ran off the right of the graph.
				k1end += 2
			} else if y1 > textBLen {
				// Ran off the bottom of the graph.
				k1start += 2
			} else if front {
				k2_offset := v_offset + delta - k1
				if k2_offset >= 0 && k2_offset < v_length && v2[k2_offset] != -1 {
					// Mirror x2 onto top-left coordinate system.
					x2 := textALen - v2[k2_offset]
					if x1 >= x2 {
						// Overlap detected.
						return dmp.diff_bisectSplit(textA, textB, x1, y1, deadline)
					}
				}
			}
		}

		// Walk the reverse path one step.
		for k2 := -d + k2start; k2 <= d-k2end; k2 += 2 {
			k2_offset := v_offset + k2
			var x2 int
			if k2 == -d || (k2 != d && v2[k2_offset-1] < v2[k2_offset+1]) {
				x2 = v2[k2_offset+1]
			} else {
				x2 = v2[k2_offset-1] + 1
			}
			y2 := x2 - k2
			for x2 < textALen && y2 < textBLen && textA[textALen-x2-1] == textB[textBLen-y2-1] {
				x2++
				y2++
			}
			v2[k2_offset] = x2
			if x2 > textALen {
				// Ran off the left of the graph.
				k2end += 2
			} else if y2 > textBLen {
				// Ran off the top of the graph.
				k2start += 2
			} else if !front {
				k1_offset := v_offset + delta - k2
				if k1_offset >= 0 && k1_offset < v_length && v1[k1_offset] != -1 {
					x1 := v1[k1_offset]
					y1 := v_offset + x1 - k1_offset
					// Mirror x2 onto top-left coordinate system.
					x2 = textALen - x2
					if x1 >= x2 {
						// Overlap detected.
						return dmp.diff_bisectSplit(textA, textB, x1, y1, deadline)
					}
				}
			}
		}

	}

	// Diff took too long and hit the deadline or
	// number of diffs equals number of characters, no commonality at all.
	var diffs []Diff
	diffs = append(diffs, Diff{DELETE, textA})
	diffs = append(diffs, Diff{INSERT, textB})
	return diffs
}

// * diff_bisectSplit
func (dmp *DiffMatchPatch) diff_bisectSplit(textA, textB string, x, y int, deadline time.Time) []Diff {
	textA1 := textA[:x]
	textB1 := textB[:y]
	textA2 := textA[x:]
	textB2 := textB[y:]

	// Compute both diffs serially.
	_, diffs := dmp.diffMainDeadline(textA1, textB1, false, deadline)
	_, diffsb := dmp.diffMainDeadline(textA2, textB2, false, deadline)

	return append(diffs, diffsb...)
}

// * diff_linesToChars
func (dmp *DiffMatchPatch) diff_linesToChars(textA, textB string) ([]uint32, []uint32, []string) {

	// "\x00" is a valid character, but various debuggers don't like it.
	// So we'll insert a junk entry to avoid generating a null character.
	lineArray := []string{""}
	lineHash := make(map[string]int)
	// e.g. linearray[4] == "Hello\n"
	// e.g. linehash.get("Hello\n") == 4

	chars1 := dmp.diff_linesToCharsMunge(textA, lineArray, lineHash)
	chars2 := dmp.diff_linesToCharsMunge(textB, lineArray, lineHash)

	return chars1, chars2, lineArray
}

// * diff_linestoCharsMunge
func (dmp *DiffMatchPatch) diff_linesToCharsMunge(text string, lineArray []string, lineHash map[string]int) []uint32 {
	lineStart := 0
	lineEnd := -1
	chars := []uint32{}

	// Walk the text, pulling out a substring for each line.
	// text.split('\n') would would temporarily double our memory footprint.
	// Modifying text would create many large strings to garbage collect.
	for lineEnd < len(text)-1 {
		lineEnd = strings.Index(text[lineStart:], "\n")
		if lineEnd == -1 {
			lineEnd = len(text) - 1
		}
		line := text[lineStart : lineEnd+1]
		lineStart = lineEnd + 1
		lineValue, ok := lineHash[line]

		if ok {
			chars = append(chars, uint32(lineValue))
		} else {
			lineArray = append(lineArray, line)
			lineHash[line] = len(lineArray) - 1
			chars = append(chars, uint32(len(lineArray)-1))
		}
	}
	return chars
}

// * diff_charsToLines
func (dmp *DiffMatchPatch) diff_charsToLines(diffs []Diff, lineArray []string) []Diff {
	diffsWithText := make([]Diff, 0, len(diffs))

	for _, diff := range diffs {
		text := make([]string, len(diff.Text))

		for y := 0; y < len(diff.Text); y++ {
			text[y] = lineArray[uint32(diff.Text[y])]
		}
		diff.Text = strings.Join(text, "")
		diffsWithText = append(diffsWithText, diff)
	}

	return diffsWithText
}

// * diff_commonPrefix
func (dmp *DiffMatchPatch) diff_commonPrefix(textA, textB string) int {
	// Performance analysis: http://neil.fraser.name/news/2007/10/09/
	n := min(len(textA), len(textB))
	for i := 0; i < n; i++ {
		if textA[i] != textB[i] {
			return i
		}
	}
	return n
}

// * diff_commonSuffix
func (dmp *DiffMatchPatch) diff_commonSuffix(textA, textB string) int {
	// Performance analysis: http://neil.fraser.name/news/2007/10/09/
	textALen := len(textA)
	textBLen := len(textB)
	n := min(textALen, textBLen)

	for i := 1; i < n; i++ {
		if textA[textALen-i] != textB[textBLen-i] {
			return i - 1
		}
	}
	return n
}

// * diff_commonOverlap
func (dmp *DiffMatchPatch) diff_commonOverlap(textA, textB string) int {
	// Cache the text lengths to prevent multiple calls.
	textALen := len(textA)
	textBLen := len(textB)
	// Eliminate the null case.
	if textALen == 0 || textBLen == 0 {
		return 0
	}
	// Truncate the longer string.
	textA_trunc := textA
	textB_trunc := textB
	if textALen > textBLen {
		textA_trunc = textA[textALen-textBLen:]
	} else if textALen < textBLen {
		textB_trunc = textB[:textALen]
	}
	text_length := min(textALen, textBLen)
	// Quick check for the worst case.
	if textA_trunc == textB_trunc {
		return text_length
	}

	// Start by looking for a single character match
	// and increase length until no match is found.
	// Performance analysis: http://neil.fraser.name/news/2010/11/04/
	best := 0
	length := 1
	for {
		pattern := textA_trunc[len(textA_trunc)-length:]
		found := strings.Index(textB_trunc, pattern)
		if found == -1 {
			return best
		}
		length += found
		if found == 0 || textA_trunc[len(textA_trunc)-length:] == textB_trunc[:length] {
			best = length
			length++
		}
	}

	return best
}

// * diff_halfMatch

// * diff_cleanupMerge

// * diff_cleanupSemantic
// * diff_prettyHtml
// Convert a diff array into a pretty HTML report.
func diff_prettyHtml(diffs []Diff) string {
	var html []string
	// var pattern_amp = /&/g;
	// var pattern_lt = /</g;
	// var pattern_gt = />/g;
	// var pattern_para = /\n/g;
	// for (var x = 0; x < diffs.length; x++) {
	// 	var op = diffs[x][0];    // Operation (insert, delete, equal)
	// 	var data = diffs[x][1];  // Text of change.
	// 	var text = data.replace(pattern_amp, '&amp;').replace(pattern_lt, '&lt;')
	// 			.replace(pattern_gt, '&gt;').replace(pattern_para, '&para;<br>');
	// 	switch (op) {
	// 		case DIFF_INSERT:
	// 			html[x] = '<ins style="background:#e6ffe6;">' + text + '</ins>';
	// 			break;
	// 		case DIFF_DELETE:
	// 			html[x] = '<del style="background:#ffe6e6;">' + text + '</del>';
	// 			break;
	// 		case DIFF_EQUAL:
	// 			html[x] = '<span>' + text + '</span>';
	// 			break;
	// 	}
	// }
	text := "test!"
	html = append(html, "<span>", text, "</span>")
	return strings.Join(html, "")
}

// * diff_cleanupSemanticLossless
