package diff

import (
	"bytes"
	"errors"
	"html"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
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
	_, diffs = dmp.diff_cleanupMerge(diffs)

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
		return dmp.diff_lineMode(textA, textB, deadline)
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
				endDiffs := diffs[pointer:]
				diffs = diffs[:pointer-count_delete-count_insert]
				diffs = append(diffs, endDiffs...)
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
		// line = safeMid(text, lineStart, lineEnd + 1 - lineStart);
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
}

// * diff_halfMatch
func (dmp *DiffMatchPatch) diff_halfMatch(textA, textB string) (string, string, string, string, string) {
	if dmp.Diff_Timeout <= 0 {
		// Don't risk returning a non-optimal diff if we have unlimited time.
		return "", "", "", "", ""
	}

	var longtext, shorttext string
	if len(textA) > len(textB) {
		longtext = textA
		shorttext = textB
	} else {
		longtext = textB
		shorttext = textA
	}

	if len(longtext) < 4 || len(shorttext)*2 < len(longtext) {
		return "", "", "", "", "" // Pointless.
	}

	var hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common string
	// First check if the second quarter is the seed for a half-match.
	hm1_best_longtext_a, hm1_best_longtext_b, hm1_best_shorttext_a, hm1_best_shorttext_b, hm1_best_common := dmp.diff_halfMatchI(longtext, shorttext,
		(len(longtext)+3)/4)
	// Check again based on the third quarter.
	hm2_best_longtext_a, hm2_best_longtext_b, hm2_best_shorttext_a, hm2_best_shorttext_b, hm2_best_common := dmp.diff_halfMatchI(longtext, shorttext,
		(len(longtext)+1)/2)
	if len(hm1_best_common) == 0 && len(hm2_best_common) == 0 {
		return "", "", "", "", ""
	} else if len(hm2_best_common) == 0 {
		hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common = hm1_best_longtext_a, hm1_best_longtext_b, hm1_best_shorttext_a, hm1_best_shorttext_b, hm1_best_common
	} else if len(hm1_best_common) == 0 {
		hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common = hm2_best_longtext_a, hm2_best_longtext_b, hm2_best_shorttext_a, hm2_best_shorttext_b, hm2_best_common
	} else {
		// Both matched.  Select the longest.
		if len(hm1_best_common) > len(hm2_best_common) {
			hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common = hm1_best_longtext_a, hm1_best_longtext_b, hm1_best_shorttext_a, hm1_best_shorttext_b, hm1_best_common
		} else {
			hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common = hm2_best_longtext_a, hm2_best_longtext_b, hm2_best_shorttext_a, hm2_best_shorttext_b, hm2_best_common
		}
	}

	// A half-match was found, sort out the return data.
	if len(textA) > len(textB) {
		return hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common
	} else {
		// return []string{hm[2], hm[3], hm[0], hm[1], hm[4]}
		return hm_best_shorttext_a, hm_best_shorttext_b, hm_best_longtext_a, hm_best_longtext_b, hm_best_common
	}
}

// * diff_halfMatchI
func (dmp *DiffMatchPatch) diff_halfMatchI(longtext, shorttext string, i int) (string, string, string, string, string) {
	// Start with a 1/4 length substring at position i as a seed.
	seed := longtext[i : i+len(longtext)/4]

	// line = safeMid(text, lineStart, lineEnd + 1 - lineStart);
	// line := text[lineStart : lineEnd+1]
	j := -1
	var best_common string
	var best_longtext_a, best_longtext_b string
	var best_shorttext_a, best_shorttext_b string

	for j = strings.Index(shorttext[j+1:], seed); j != -1; j = strings.Index(shorttext[j+1:], seed) {
		prefixLength := dmp.diff_commonPrefix(longtext[i:], shorttext[j:])
		suffixLength := dmp.diff_commonSuffix(longtext[:i], shorttext[:j])
		if len(best_common) < suffixLength+prefixLength {
			best_common = shorttext[j-suffixLength:] + shorttext[j:j+prefixLength]
			best_longtext_a = longtext[:i-suffixLength]
			best_longtext_b = longtext[i+prefixLength:]
			best_shorttext_a = shorttext[:j-suffixLength]
			best_shorttext_b = shorttext[j+prefixLength:]
		}
	}
	if len(best_common)*2 >= len(longtext) {
		return best_longtext_a, best_longtext_b, best_shorttext_a, best_shorttext_b, best_common
	} else {
		return "", "", "", "", ""
	}
}

// * diff_cleanupSemantic
func (dmp *DiffMatchPatch) diff_cleanupSemantic(diffs []Diff) []Diff {
	if len(diffs) == 0 {
		return []Diff{}
	}
	changes := false
	equalities := make([]int, 0, len(diffs)) // Stack of equalities.
	var lastequality string                  // Always equal to equalities.lastElement().text
	pointer := 0

	// Number of characters that changed prior to the equality.
	length_insertions1 := 0
	length_deletions1 := 0
	// Number of characters that changed after the equality.
	length_insertions2 := 0
	length_deletions2 := 0
	for pointer < len(diffs) {
		if diffs[pointer].Type == EQUAL {
			// Equality found.
			equalities = append(equalities, pointer)
			length_insertions1 = length_insertions2
			length_deletions1 = length_deletions2
			length_insertions2 = 0
			length_deletions2 = 0
			lastequality = diffs[pointer].Text
		} else {
			// An insertion or deletion.
			if diffs[pointer].Type == INSERT {
				length_insertions2 += len(diffs[pointer].Text)
			} else {
				length_deletions2 += len(diffs[pointer].Text)
			}
			// Eliminate an equality that is smaller or equal to the edits on both
			// sides of it.
			if len(lastequality) > 0 && len(lastequality) <= int(math.Max(float64(length_insertions1), float64(length_deletions1))) && len(lastequality) <= int(math.Max(float64(length_insertions2), float64(length_deletions2))) {
				// printf("Splitting: '%s'\n", qPrintable(lastequality));
				// Walk back to offending equality.
				lastPointer := equalities[len(equalities)-1]
				// Replace equality with a delete.
				diffs[lastPointer].Type = DELETE
				diffs[lastPointer].Text = lastequality

				// Insert a corresponding an insert.
				diffs[pointer+1].Type = INSERT

				equalities = equalities[:len(equalities)-1] // Throw away the equality we just deleted.
				if len(equalities) > 0 {
					// Throw away the previous equality (it needs to be reevaluated).
					equalities = equalities[:len(equalities)-1]
				}
				pointer = -1
				if len(equalities) > 0 {
					// There are no previous equalities, walk back to the start.
					pointer = equalities[len(equalities)-1]
				}

				length_insertions1 = 0 // Reset the counters.
				length_deletions1 = 0
				length_insertions2 = 0
				length_deletions2 = 0
				lastequality = ""
				changes = true
			}
		}
		pointer++
	}

	// Normalize the diff.
	if changes {
		_, diffs = dmp.diff_cleanupMerge(diffs)
	}
	diffs = dmp.diff_cleanupSemanticLossless(diffs)

	// Find any overlaps between deletions and insertions.
	// e.g: <del>abcxxx</del><ins>xxxdef</ins>
	//   -> <del>abc</del>xxx<ins>def</ins>
	// e.g: <del>xxxabc</del><ins>defxxx</ins>
	//   -> <ins>def</ins>xxx<del>abc</del>
	// Only extract an overlap if it is as big as the edit ahead or behind it.
	pointer = 1

	for pointer < len(diffs) {
		if diffs[pointer-1].Type == DELETE && diffs[pointer].Type == INSERT {
			deletion := diffs[pointer-1].Text
			insertion := diffs[pointer].Text
			overlap_length1 := dmp.diff_commonOverlap(deletion, insertion)
			overlap_length2 := dmp.diff_commonOverlap(insertion, deletion)
			if overlap_length1 >= overlap_length2 {
				if overlap_length1 >= len(deletion)/2.0 ||
					overlap_length1 >= len(insertion)/2.0 {
					// Overlap found.  Insert an equality and trim the surrounding edits.
					preDiffs := diffs[0 : pointer-1]
					postDiffs := diffs[pointer:0]
					// diffs = splice(diffs, pointer, 0, Diff{EQUAL, insertion[:overlap_length1]})
					diffs = append(preDiffs, Diff{EQUAL, insertion[:overlap_length1]})
					diffs = append(diffs, postDiffs...)
					diffs[pointer-1].Text = deletion[0 : len(deletion)-overlap_length1]
					diffs[pointer+1].Text = insertion[overlap_length1:]
					pointer++
				}
			} else {
				if overlap_length2 >= len(deletion)/2.0 ||
					overlap_length2 >= len(insertion)/2.0 {
					// Reverse overlap found.
					// Insert an equality and swap and trim the surrounding edits.
					overlap := Diff{EQUAL, deletion[:overlap_length2]}
					//diffs = splice(diffs, pointer, 0, overlap)
					preDiffs := diffs[0 : pointer-1]
					postDiffs := diffs[pointer:0]
					// diffs = splice(diffs, pointer, 0, overlap)
					diffs = append(preDiffs, overlap)
					diffs = append(diffs, postDiffs...)
					diffs[pointer-1].Type = INSERT
					diffs[pointer-1].Text = insertion[0 : len(insertion)-overlap_length2]
					diffs[pointer+1].Type = DELETE
					diffs[pointer+1].Text = deletion[overlap_length2:]
					pointer++
				}
			}
			pointer++
		}
		pointer++
	}
	return diffs
}

// * diff_cleanupSemanticLossless
func (dmp *DiffMatchPatch) diff_cleanupSemanticLossless(diffs []Diff) []Diff {
	var equality1, edit, equality2 string
	// Create a new iterator at the start.
	pointer := 1

	// Intentionally ignore the first and last element (don't need checking).
	for pointer < len(diffs)-1 {
		if diffs[pointer-1].Type == EQUAL &&
			diffs[pointer+1].Type == EQUAL {
			// This is a single edit surrounded by equalities.
			equality1 = diffs[pointer-1].Text
			edit = diffs[pointer].Text
			equality2 = diffs[pointer+1].Text

			// First, shift the edit as far left as possible.
			commonOffset := dmp.diff_commonSuffix(equality1, edit)
			if commonOffset > 0 {
				commonString := edit[len(edit)-commonOffset:]
				equality1 = equality1[:len(equality1)-commonOffset]
				edit = commonString + edit[:len(edit)-commonOffset]
				equality2 = commonString + equality2
			}

			// Second, step character by character right, looking for the best fit.
			bestEquality1 := equality1
			bestEdit := edit
			bestEquality2 := equality2
			bestScore := dmp.diff_cleanupSemanticScore(equality1, edit) +
				dmp.diff_cleanupSemanticScore(edit, equality2)
			for len(edit) != 0 && len(equality2) != 0 &&
				edit[0] == equality2[0] {
				_, sz := utf8.DecodeRuneInString(edit)
				if len(equality2) < sz || edit[:sz] != equality2[:sz] {
					break
				}
				equality1 += edit[:sz]
				edit = edit[sz:] + equality2[:sz]
				equality2 = equality2[sz:]
				score := dmp.diff_cleanupSemanticScore(equality1, edit) +
					dmp.diff_cleanupSemanticScore(edit, equality2)
				// The >= encourages trailing rather than leading whitespace on edits.
				if score >= bestScore {
					bestScore = score
					bestEquality1 = equality1
					bestEdit = edit
					bestEquality2 = equality2
				}
			}

			if diffs[pointer-1].Text != bestEquality1 {
				// We have an improvement, save it back to the diff.
				if len(bestEquality1) > 0 {
					diffs[pointer-1].Text = bestEquality1
				} else {
					diffs = append(diffs[:pointer-1], diffs[pointer:]...)
					pointer--
				}
				diffs[pointer].Text = bestEdit
				if len(bestEquality2) > 0 {
					diffs[pointer+1].Text = bestEquality2
				} else {
					diffs = append(diffs[:pointer+1], diffs[pointer+2:]...)
					pointer--
				}
			}
		}
		pointer++
	}
	return diffs
}

// Thanks to sergi for the hints:
var (
	nonAlphaNumericRegex = regexp.MustCompile(`[^a-zA-Z0-9]`)
	whitespaceRegex      = regexp.MustCompile(`\s`)
	linebreakRegex       = regexp.MustCompile(`[\r\n]`)
	blanklineEndRegex    = regexp.MustCompile(`\n\r?\n$`)
	blanklineStartRegex  = regexp.MustCompile(`^\r?\n\r?\n`)
)

// * diff_cleanupSemanticScore
func (dmp *DiffMatchPatch) diff_cleanupSemanticScore(one, two string) int {
	if len(one) == 0 || len(two) == 0 {
		// Edges are the best.
		return 6
	}

	// Each port of this function behaves slightly differently due to
	// subtle differences in each language's definition of things like
	// 'whitespace'.  Since this function's purpose is largely cosmetic,
	// the choice has been made to use each language's native features
	// rather than force total conformity.
	rune1, _ := utf8.DecodeLastRuneInString(one)
	rune2, _ := utf8.DecodeRuneInString(two)
	char1 := string(rune1)
	char2 := string(rune2)

	nonAlphaNumeric1 := nonAlphaNumericRegex.MatchString(char1)
	nonAlphaNumeric2 := nonAlphaNumericRegex.MatchString(char2)
	whitespace1 := nonAlphaNumeric1 && whitespaceRegex.MatchString(char1)
	whitespace2 := nonAlphaNumeric2 && whitespaceRegex.MatchString(char2)
	lineBreak1 := whitespace1 && linebreakRegex.MatchString(char1)
	lineBreak2 := whitespace2 && linebreakRegex.MatchString(char2)
	blankLine1 := lineBreak1 && blanklineEndRegex.MatchString(one)
	blankLine2 := lineBreak2 && blanklineEndRegex.MatchString(two)

	if blankLine1 || blankLine2 {
		// Five points for blank lines.
		return 5
	} else if lineBreak1 || lineBreak2 {
		// Four points for line breaks.
		return 4
	} else if nonAlphaNumeric1 && !whitespace1 && whitespace2 {
		// Three points for end of sentences.
		return 3
	} else if whitespace1 || whitespace2 {
		// Two points for whitespace.
		return 2
	} else if nonAlphaNumeric1 || nonAlphaNumeric2 {
		// One point for non-alphanumeric.
		return 1
	}
	return 0
}

// * diff_cleanupEfficiency - used by patch, skip

// * diff_xIndex - used by patch, skip
// * diff_cleanupMerge
func (dmp *DiffMatchPatch) diff_cleanupMerge(diffs []Diff) (error, []Diff) {
	diffs = append(diffs, Diff{EQUAL, ""}) // Add a dummy entry at the end.
	pointer := 0
	count_delete := 0
	count_insert := 0
	text_delete := ""
	text_insert := ""
	var commonlength int
	for pointer < len(diffs) {
		switch diffs[pointer].Type {
		case INSERT:
			count_insert++
			text_insert += diffs[pointer].Text
			pointer++
			break
		case DELETE:
			count_delete++
			text_delete += diffs[pointer].Text
			pointer++
			break
		case EQUAL:
			if count_delete+count_insert > 1 {
				both_types := count_delete != 0 && count_insert != 0
				// Delete the offending records.
				tempPointer := pointer - count_delete - count_insert
				if both_types {
					// Factor out any common prefixies.
					commonlength = dmp.diff_commonPrefix(text_insert, text_delete)
					if commonlength != 0 {
						if tempPointer > 0 {
							if diffs[tempPointer-1].Type != EQUAL {
								return errors.New("Previous diff should have been an equality."), nil
							}
							diffs[tempPointer-1].Text += text_insert[:commonlength]
						} else {
							diffs = append([]Diff{{EQUAL, text_insert[:commonlength]}}, diffs...)
							pointer++
						}
						text_insert = text_insert[commonlength:]
						text_delete = text_delete[commonlength:]
					}
					// Factor out any common suffixies.
					commonlength = dmp.diff_commonSuffix(text_insert, text_delete)
					if commonlength != 0 {
						diffs[pointer].Text = text_insert[len(text_insert)-commonlength:] + diffs[pointer].Text
						text_insert = text_insert[:len(text_insert)-commonlength]
						text_delete = text_delete[:len(text_delete)-commonlength]
					}
				}
				// Insert the merged records.
				if len(text_delete) == 0 {
					diffs = append(diffs[:pointer-count_insert], append([]Diff{Diff{INSERT, text_insert}}, diffs[pointer:]...)...)
				} else if len(text_insert) == 0 {
					diffs = append(diffs[:pointer-count_delete], append([]Diff{Diff{DELETE, text_delete}}, diffs[pointer:]...)...)
				} else {
					diffs = append(diffs[:pointer-count_delete-count_insert],
						append([]Diff{Diff{INSERT, text_insert}, Diff{DELETE, text_delete}}, diffs[pointer:]...)...)
				}
				// Step forward to the equality.
				pointer = pointer - count_delete - count_insert + 1
				if count_delete != 0 {
					pointer++
				}
				if count_insert != 0 {
					pointer++
				}

			} else if pointer != 0 && diffs[pointer-1].Type == EQUAL {
				// Merge this equality with the previous one.
				diffs[pointer-1].Text += diffs[pointer].Text
				diffs = append(diffs[:pointer], diffs[pointer+1:]...)
			} else {
				pointer++
			}
			count_insert = 0
			count_delete = 0
			text_delete = ""
			text_insert = ""
			break
		}
	}
	if len(diffs[len(diffs)-1].Text) == 0 {
		diffs = diffs[0 : len(diffs)-1] // Remove the dummy entry at the end.
	}

	/*
	 * Second pass: look for single edits surrounded on both sides by equalities
	 * which can be shifted sideways to eliminate an equality.
	 * e.g: A<ins>BA</ins>C -> <ins>AB</ins>AC
	 */
	changes := false
	// Create a new iterator at the start.
	// (As opposed to walking the current one back.)
	pointer = 1

	// Intentionally ignore the first and last element (don't need checking).
	for pointer < len(diffs)-1 {
		if diffs[pointer-1].Type == EQUAL &&
			diffs[pointer+1].Type == EQUAL {
			// This is a single edit surrounded by equalities.
			if strings.HasSuffix(diffs[pointer].Text, diffs[pointer-1].Text) {
				// Shift the edit over the previous equality.
				diffs[pointer].Text = diffs[pointer-1].Text +
					diffs[pointer].Text[:len(diffs[pointer].Text)-
						len(diffs[pointer-1].Text)]
				diffs[pointer+1].Text = diffs[pointer-1].Text + diffs[pointer+1].Text
				// Delete prevDiff.
				diffs = append(diffs[:pointer-1], diffs[pointer:]...)
				changes = true
			} else if strings.HasPrefix(diffs[pointer].Text, diffs[pointer+1].Text) {
				// Shift the edit over the next equality.
				diffs[pointer-1].Text += diffs[pointer+1].Text
				diffs[pointer].Text = diffs[pointer].Text[len(diffs[pointer+1].Text):] + diffs[pointer+1].Text
				diffs = append(diffs[:pointer+1], diffs[pointer+2:]...)
				changes = true
			}
		}
		pointer++
	}
	// If shifts were made, the diff needs reordering and another shift sweep.
	if changes {
		_, diffs = dmp.diff_cleanupMerge(diffs)
	}
	return nil, diffs
}

// * diff_prettyHtml
// Convert a diff array into a pretty HTML report.
func diff_prettyHtml(diffs []Diff) string {
	var buffer bytes.Buffer
	for _, diff := range diffs {
		text := strings.Replace(html.EscapeString(diff.Text), "\n", "&para;<br>", -1)
		switch diff.Type {
		case INSERT:
			_, _ = buffer.WriteString("<ins style=\"background:#e6ffe6;\">")
			_, _ = buffer.WriteString(text)
			_, _ = buffer.WriteString("</ins>")
		case DELETE:
			_, _ = buffer.WriteString("<del style=\"background:#ffe6e6;\">")
			_, _ = buffer.WriteString(text)
			_, _ = buffer.WriteString("</del>")
		case EQUAL:
			_, _ = buffer.WriteString("<span>")
			_, _ = buffer.WriteString(text)
			_, _ = buffer.WriteString("</span>")
		}
	}
	return buffer.String()
}
