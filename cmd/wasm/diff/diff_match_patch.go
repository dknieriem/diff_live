package diff

import (
	"bytes"
	"errors"
	"fmt"
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
func (dmp *DiffMatchPatch) DiffRecurse(inputA, inputB string) (error, []Diff) {
	return dmp.DiffMain([]rune(inputA), []rune(inputB), true)
}

// Recursive diff method setting a deadline
func (dmp *DiffMatchPatch) DiffMain(inputA, inputB []rune, checklines bool) (error, []Diff) {
	var deadline time.Time

	if dmp.Diff_Timeout > 0 {
		deadline = time.Now().Add(dmp.Diff_Timeout)
	}

	return dmp.DiffMainDeadline(inputA, inputB, checklines, deadline)
}

// Diff method with deadline
func (dmp *DiffMatchPatch) DiffMainDeadline(inputA, inputB []rune, checklines bool, deadline time.Time) (error, []Diff) {
	// Check for equality (speedup).
	if string(inputA) == string(inputB) {
		var diffs []Diff
		if len(inputA) > 0 {
			diffs = append(diffs, Diff{EQUAL, string(inputA)})
		}
		return nil, diffs
	}

	// Trim off common prefix (speedup).
	commonLength := dmp.DiffCommonPrefix(inputA, inputB)
	commonPrefix := inputA[:commonLength]
	textChoppedA := inputA[commonLength:]
	textChoppedB := inputB[commonLength:]

	// Trim off common suffix (speedup).
	commonLength = dmp.DiffCommonSuffix(textChoppedA, textChoppedB)
	commonSuffix := textChoppedA[len(inputA)-commonLength:]
	textChoppedA = textChoppedA[:len(inputA)-commonLength]
	textChoppedB = textChoppedB[:len(inputB)-commonLength]

	// Compute the diff on the middle block.
	diffs := dmp.DiffCompute(textChoppedA, textChoppedB, checklines, deadline)

	// Restore the prefix and suffix.
	if len(commonPrefix) > 0 {
		diffs = append([]Diff{{EQUAL, string(commonPrefix)}}, diffs...)
	}
	if len(commonSuffix) > 0 {
		diffs = append(diffs, Diff{EQUAL, string(commonSuffix)})
	}
	_, diffs = dmp.DiffCleanupMerge(diffs)

	return nil, diffs
}

// * diffCompute_
func (dmp *DiffMatchPatch) DiffCompute(textA, textB []rune, checklines bool, deadline time.Time) []Diff {
	diffs := []Diff{}

	if len(textA) > 0 {
		// Just add some text (speedup).
		diffs = append(diffs, Diff{INSERT, string(textB)})
		return diffs
	}

	if len(textB) > 0 {
		// Just delete some text (speedup).
		diffs = append(diffs, Diff{DELETE, string(textA)})
		return diffs
	}

	var longtext, shorttext []rune
	if len(textA) > len(textB) {
		longtext = textA
		shorttext = textB
	} else {
		longtext = textB
		shorttext = textA
	}
	foundTextIndex := strings.Index(string(longtext), string(shorttext))
	if foundTextIndex != -1 {
		// Shorter text is inside the longer text (speedup).
		var op Operation

		if len(textA) > len(textB) {
			op = DELETE
		} else {
			op = INSERT
		}
		diffs = append(diffs, Diff{op, string(longtext[:foundTextIndex])})
		diffs = append(diffs, Diff{EQUAL, string(shorttext)})
		diffs = append(diffs, Diff{op, string(longtext[foundTextIndex+len(shorttext):])})
		return diffs
	}

	if len(shorttext) == 1 {
		// Single character string.
		// After the previous speedup, the character can't be an equality.
		diffs = append(diffs, Diff{DELETE, string(textA)})
		diffs = append(diffs, Diff{INSERT, string(textB)})
		return diffs
	}

	// Check to see if the problem can be split in two.
	textA_1, textA_2, textB_1, textB_2, midCommon := dmp.DiffHalfMatch(textA, textB)

	if len(textA_1) > 0 {
		// A half-match was found.
		// Send both pairs off for separate processing.
		_, diffs_a := dmp.DiffMainDeadline(textA_1, textB_1, checklines, deadline)
		_, diffs_b := dmp.DiffMainDeadline(textA_2, textB_2, checklines, deadline)

		// Merge the results.
		diffs = append(diffs_a, Diff{EQUAL, string(midCommon)})
		diffs = append(diffs, diffs_b...)
		return diffs
	}

	// Perform a real diff.
	if checklines && len(textA) > 100 && len(textB) > 100 {
		return dmp.DiffLineMode(textA, textB, deadline)
	}

	return dmp.DiffBisect_(string(textA), string(textB), deadline)
}

// * diffLineMode_
func (dmp *DiffMatchPatch) DiffLineMode(textA, textB []rune, deadline time.Time) []Diff {
	// Scan the text on a line-by-line basis first.
	textA, textB, lineArray := dmp.DiffLinesToChars(string(textA), string(textB))

	_, diffs := dmp.DiffMainDeadline(textA, textB, false, deadline)

	// Convert the diff back to original text.
	diffs = dmp.DiffCharsToLines(diffs, lineArray)
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
				_, newDiffs := dmp.DiffMainDeadline([]rune(text_delete), []rune(text_insert), false, deadline)
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

// * diffBisect_
func (dmp *DiffMatchPatch) DiffBisect_(textA, textB string, deadline time.Time) []Diff {
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
						return dmp.DiffBisectSplit([]rune(textA), []rune(textB), x1, y1, deadline)
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
						return dmp.DiffBisectSplit([]rune(textA), []rune(textB), x1, y1, deadline)
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

// * diffBisectSplit
func (dmp *DiffMatchPatch) DiffBisectSplit(textA, textB []rune, x, y int, deadline time.Time) []Diff {
	textA1 := textA[:x]
	textB1 := textB[:y]
	textA2 := textA[x:]
	textB2 := textB[y:]

	// Compute both diffs serially.
	_, diffs := dmp.DiffMainDeadline(textA1, textB1, false, deadline)
	_, diffsb := dmp.DiffMainDeadline(textA2, textB2, false, deadline)

	return append(diffs, diffsb...)
}

// * diffLinesToChars - ********* @todo: fix this!! ********
func (dmp *DiffMatchPatch) DiffLinesToChars(textA, textB string) ([]rune, []rune, []string) {

	// "\x00" is a valid character, but various debuggers don't like it.
	// So we'll insert a junk entry to avoid generating a null character.
	lineArray := []string{""}
	lineHash := make(map[string]int)
	// e.g. linearray[4] == "Hello\n"
	// e.g. linehash.get("Hello\n") == 4

	chars1 := dmp.DiffLinesToCharsMunge(textA, lineArray, lineHash)
	chars2 := dmp.DiffLinesToCharsMunge(textB, lineArray, lineHash)

	return chars1, chars2, lineArray
}

// These constants define the number of bits representable
// in 1,2,3,4 byte utf8 sequences, respectively.
const ONE_BYTE_BITS = 7
const TWO_BYTE_BITS = 11
const THREE_BYTE_BITS = 16
const FOUR_BYTE_BITS = 21

const UNICODE_INVALID_RANGE_START = 0xD800
const UNICODE_INVALID_RANGE_END = 0xDFFF
const UNICODE_INVALID_RANGE_DELTA = UNICODE_INVALID_RANGE_END - UNICODE_INVALID_RANGE_START + 1
const UNICODE_RANGE_MAX = 0x10FFFF

func (dmp *DiffMatchPatch) DiffLinesToStrings(text1, text2 string) (string, string, []string) {
	// '\x00' is a valid character, but various debuggers don't like it. So we'll insert a junk entry to avoid generating a null character.
	lineArray := []string{""} // e.g. lineArray[4] == 'Hello\n'

	lineHash := make(map[string]int)
	//Each string has the index of lineArray which it points to
	strIndexArray1 := dmp.DiffLinesToStringsMunge(text1, &lineArray, lineHash)
	strIndexArray2 := dmp.DiffLinesToStringsMunge(text2, &lineArray, lineHash)

	return intArrayToString(strIndexArray1), intArrayToString(strIndexArray2), lineArray
}

func intArrayToString(ns []uint32) string {
	if len(ns) == 0 {
		return ""
	}

	b := []rune{}
	for _, n := range ns {
		b = append(b, intToRune(n))
	}
	return string(b)
}

func getBits(i uint32, cnt byte, from byte) byte {
	return byte((i >> from) & ((1 << cnt) - 1))
}

func intToRune(i uint32) rune {
	if i < (1 << ONE_BYTE_BITS) {
		return rune(i)
	}

	if i < (1 << TWO_BYTE_BITS) {
		r, size := utf8.DecodeRune([]byte{0b11000000 | getBits(i, 5, 6), 0b10000000 | getBits(i, 6, 0)})
		if size != 2 || r == utf8.RuneError {
			panic(fmt.Sprintf("Error encoding an int %d with size 2, got rune %v and size %d", size, r, i))
		}
		return r
	}

	// Last -3 here needed because for some reason 3rd to last codepoint 65533 in this range
	// was returning utf8.RuneError during encoding.
	if i < ((1 << THREE_BYTE_BITS) - UNICODE_INVALID_RANGE_DELTA - 3) {
		if i >= UNICODE_INVALID_RANGE_START {
			i += UNICODE_INVALID_RANGE_DELTA
		}

		r, size := utf8.DecodeRune([]byte{0b11100000 | getBits(i, 4, 12), 0b10000000 | getBits(i, 6, 6), 0b10000000 | getBits(i, 6, 0)})
		if size != 3 || r == utf8.RuneError {
			panic(fmt.Sprintf("Error encoding an int %d with size 3, got rune %v and size %d", size, r, i))
		}
		return r
	}

	if i < (1<<FOUR_BYTE_BITS - UNICODE_INVALID_RANGE_DELTA - 3) {
		i += UNICODE_INVALID_RANGE_DELTA + 3
		r, size := utf8.DecodeRune([]byte{0b11110000 | getBits(i, 3, 18), 0b10000000 | getBits(i, 6, 12), 0b10000000 | getBits(i, 6, 6), 0b10000000 | getBits(i, 6, 0)})
		if size != 4 || r == utf8.RuneError {
			panic(fmt.Sprintf("Error encoding an int %d with size 4, got rune %v and size %d", size, r, i))
		}
		return r
	}
	panic(fmt.Sprintf("The integer %d is too large for runeToInt()", i))
}

func (dmp *DiffMatchPatch) DiffLinesToStringsMunge(text string, lineArray *[]string, lineHash map[string]int) []uint32 {
	// Walk the text, pulling out a substring for each line. text.split('\n') would would temporarily double our memory footprint. Modifying text would create many large strings to garbage collect.
	lineStart := 0
	lineEnd := -1
	strs := []uint32{}

	for lineEnd < len(text)-1 {
		lineEnd = strings.Index(text[lineStart:], "\n")

		if lineEnd == -1 {
			lineEnd = len(text) - 1
		}

		line := text[lineStart : lineEnd+1]
		lineStart = lineEnd + 1
		lineValue, ok := lineHash[line]

		if ok {
			strs = append(strs, uint32(lineValue))
		} else {
			*lineArray = append(*lineArray, line)
			lineHash[line] = len(*lineArray) - 1
			strs = append(strs, uint32(len(*lineArray)-1))
		}
	}

	return strs
}

// * diffLinestoCharsMunge
func (dmp *DiffMatchPatch) DiffLinesToCharsMunge(text string, lineArray []string, lineHash map[string]int) []rune {
	lineStart := 0
	lineEnd := -1
	chars := []rune{}

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
			chars = append(chars, rune(lineValue))
		} else {
			lineArray = append(lineArray, line)
			lineHash[line] = len(lineArray) - 1
			chars = append(chars, rune(len(lineArray)-1))
		}
	}
	return chars
}

// * diffCharsToLines
func (dmp *DiffMatchPatch) DiffCharsToLines(diffs []Diff, lineArray []string) []Diff {
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

// * diffCommonPrefix
func (dmp *DiffMatchPatch) DiffCommonPrefix(textA, textB []rune) int {
	// Performance analysis: http://neil.fraser.name/news/2007/10/09/
	n := min(len(textA), len(textB))
	for i := 0; i < n; i++ {
		if textA[i] != textB[i] {
			return i
		}
	}
	return n
}

// * diffCommonSuffix
func (dmp *DiffMatchPatch) DiffCommonSuffix(textA, textB []rune) int {
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

func (dmp *DiffMatchPatch) DiffCommonSuffixString(textA, textB string) int {
	return dmp.DiffCommonSuffix([]rune(textA), []rune(textB))
}

// * diffCommonOverlap
func (dmp *DiffMatchPatch) DiffCommonOverlap(textA, textB string) int {
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

// * diffHalfMatch
func (dmp *DiffMatchPatch) DiffHalfMatch(textA, textB []rune) ([]rune, []rune, []rune, []rune, []rune) {
	if dmp.Diff_Timeout <= 0 {
		// Don't risk returning a non-optimal diff if we have unlimited time.
		return nil, nil, nil, nil, nil
	}

	var longtext, shorttext []rune
	if len(textA) > len(textB) {
		longtext = textA
		shorttext = textB
	} else {
		longtext = textB
		shorttext = textA
	}

	if len(longtext) < 4 || len(shorttext)*2 < len(longtext) {
		return nil, nil, nil, nil, nil // Pointless.
	}

	var hm_best_longtext_a, hm_best_longtext_b, hm_best_shorttext_a, hm_best_shorttext_b, hm_best_common []rune
	// First check if the second quarter is the seed for a half-match.
	hm1_best_longtext_a, hm1_best_longtext_b, hm1_best_shorttext_a, hm1_best_shorttext_b, hm1_best_common := dmp.DiffHalfMatchI(longtext, shorttext,
		(len(longtext)+3)/4)
	// Check again based on the third quarter.
	hm2_best_longtext_a, hm2_best_longtext_b, hm2_best_shorttext_a, hm2_best_shorttext_b, hm2_best_common := dmp.DiffHalfMatchI(longtext, shorttext,
		(len(longtext)+1)/2)
	if len(hm1_best_common) == 0 && len(hm2_best_common) == 0 {
		return nil, nil, nil, nil, nil
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

// * diffHalfMatchI
func (dmp *DiffMatchPatch) DiffHalfMatchI(longtext, shorttext []rune, i int) ([]rune, []rune, []rune, []rune, []rune) {
	// Start with a 1/4 length substring at position i as a seed.
	seed := longtext[i : i+len(longtext)/4]

	// line = safeMid(text, lineStart, lineEnd + 1 - lineStart);
	// line := text[lineStart : lineEnd+1]
	j := -1
	var best_common []rune
	var best_longtext_a, best_longtext_b []rune
	var best_shorttext_a, best_shorttext_b []rune

	for j = strings.Index(string(shorttext[j+1:]), string(seed)); j != -1; j = strings.Index(string(shorttext[j+1:]), string(seed)) {
		prefixLength := dmp.DiffCommonPrefix(longtext[i:], shorttext[j:])
		suffixLength := dmp.DiffCommonSuffix(longtext[:i], shorttext[:j])
		if len(best_common) < suffixLength+prefixLength {
			best_common = append(shorttext[j-suffixLength:j], shorttext[j:j+prefixLength]...)
			best_longtext_a = longtext[:i-suffixLength]
			best_longtext_b = longtext[i+prefixLength:]
			best_shorttext_a = shorttext[:j-suffixLength]
			best_shorttext_b = shorttext[j+prefixLength:]
		}
	}
	if len(best_common)*2 >= len(longtext) {
		return best_longtext_a, best_longtext_b, best_shorttext_a, best_shorttext_b, best_common
	} else {
		return nil, nil, nil, nil, nil
	}
}

// * diffCleanupSemantic
func (dmp *DiffMatchPatch) DiffCleanupSemantic(diffs []Diff) []Diff {
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
		_, diffs = dmp.DiffCleanupMerge(diffs)
	}
	diffs = dmp.DiffCleanupSemanticLossless(diffs)

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
			overlap_length1 := dmp.DiffCommonOverlap(deletion, insertion)
			overlap_length2 := dmp.DiffCommonOverlap(insertion, deletion)
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

// * diffCleanupSemanticLossless
func (dmp *DiffMatchPatch) DiffCleanupSemanticLossless(diffs []Diff) []Diff {
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
			commonOffset := dmp.DiffCommonSuffixString(equality1, edit)
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
			bestScore := dmp.DiffCleanupSemanticScore(equality1, edit) +
				dmp.DiffCleanupSemanticScore(edit, equality2)
			for len(edit) != 0 && len(equality2) != 0 &&
				edit[0] == equality2[0] {
				_, sz := utf8.DecodeRuneInString(edit)
				if len(equality2) < sz || edit[:sz] != equality2[:sz] {
					break
				}
				equality1 += edit[:sz]
				edit = edit[sz:] + equality2[:sz]
				equality2 = equality2[sz:]
				score := dmp.DiffCleanupSemanticScore(equality1, edit) +
					dmp.DiffCleanupSemanticScore(edit, equality2)
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

// * diffCleanupSemanticScore
func (dmp *DiffMatchPatch) DiffCleanupSemanticScore(one, two string) int {
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

// * diffCleanupEfficiency - used by patch, skip
func (dmp *DiffMatchPatch) DiffCleanupEfficiency(diffs []Diff) []Diff {
	changes := false
	// Stack of indices where equalities are found.
	type equality struct {
		data int
		next *equality
	}
	var equalities *equality
	// Always equal to equalities[equalitiesLength-1][1]
	lastequality := ""
	pointer := 0 // Index of current position.
	// Is there an insertion operation before the last equality.
	preIns := false
	// Is there a deletion operation before the last equality.
	preDel := false
	// Is there an insertion operation after the last equality.
	postIns := false
	// Is there a deletion operation after the last equality.
	postDel := false
	for pointer < len(diffs) {
		if diffs[pointer].Type == EQUAL { // Equality found.
			if len(diffs[pointer].Text) < int(dmp.Diff_EditCost) &&
				(postIns || postDel) {
				// Candidate found.
				equalities = &equality{
					data: pointer,
					next: equalities,
				}
				preIns = postIns
				preDel = postDel
				lastequality = diffs[pointer].Text
			} else {
				// Not a candidate, and can never become one.
				equalities = nil
				lastequality = ""
			}
			postIns = false
			postDel = false
		} else { // An insertion or deletion.
			if diffs[pointer].Type == DELETE {
				postDel = true
			} else {
				postIns = true
			}

			// Five types to be split:
			// <ins>A</ins><del>B</del>XY<ins>C</ins><del>D</del>
			// <ins>A</ins>X<ins>C</ins><del>D</del>
			// <ins>A</ins><del>B</del>X<ins>C</ins>
			// <ins>A</del>X<ins>C</ins><del>D</del>
			// <ins>A</ins><del>B</del>X<del>C</del>
			var sumPres int
			if preIns {
				sumPres++
			}
			if preDel {
				sumPres++
			}
			if postIns {
				sumPres++
			}
			if postDel {
				sumPres++
			}
			if len(lastequality) > 0 &&
				((preIns && preDel && postIns && postDel) ||
					((len(lastequality) < int(dmp.Diff_EditCost)/2) && sumPres == 3)) {

				insPoint := equalities.data

				// Duplicate record.
				diffs = append(diffs[:insPoint], append([]Diff{Diff{DELETE, lastequality}},
					diffs[insPoint:]...)...)

				// Change second copy to insert.
				diffs[insPoint+1].Type = INSERT
				// Throw away the equality we just deleted.
				equalities = equalities.next
				lastequality = ""

				if preIns && preDel {
					// No changes made which could affect previous entry, keep going.
					postIns = true
					postDel = true
					equalities = nil
				} else {
					if equalities != nil {
						equalities = equalities.next
					}
					if equalities != nil {
						pointer = equalities.data
					} else {
						pointer = -1
					}
					postIns = false
					postDel = false
				}
				changes = true
			}
		}
		pointer++
	}

	if changes {
		_, diffs = dmp.DiffCleanupMerge(diffs)
	}

	return diffs
}

// * diff_xIndex - used by patch, skip
// * diffCleanupMerge
func (dmp *DiffMatchPatch) DiffCleanupMerge(diffs []Diff) (error, []Diff) {
	diffs = append(diffs, Diff{EQUAL, ""}) // Add a dummy entry at the end.
	pointer := 0
	count_delete := 0
	count_insert := 0
	text_delete := []rune{}
	text_insert := []rune{}
	var commonlength int
	for pointer < len(diffs) {
		switch diffs[pointer].Type {
		case INSERT:
			count_insert++
			text_insert = append(text_insert, []rune(diffs[pointer].Text)...)
			pointer++
			break
		case DELETE:
			count_delete++
			text_delete = append(text_delete, []rune(diffs[pointer].Text)...)
			pointer++
			break
		case EQUAL:
			if count_delete+count_insert > 1 {
				both_types := count_delete != 0 && count_insert != 0
				// Delete the offending records.
				tempPointer := pointer - count_delete - count_insert
				if both_types {
					// Factor out any common prefixies.
					commonlength = dmp.DiffCommonPrefix(text_insert, text_delete)
					if commonlength != 0 {
						if tempPointer > 0 {
							if diffs[tempPointer-1].Type != EQUAL {
								return errors.New("Previous diff should have been an equality."), nil
							}
							diffs[tempPointer-1].Text += string(text_insert[:commonlength])
						} else {
							diffs = append([]Diff{{EQUAL, string(text_insert[:commonlength])}}, diffs...)
							pointer++
						}
						text_insert = text_insert[commonlength:]
						text_delete = text_delete[commonlength:]
					}
					// Factor out any common suffixies.
					commonlength = dmp.DiffCommonSuffix(text_insert, text_delete)
					if commonlength != 0 {
						diffs[pointer].Text = string(text_insert[len(text_insert)-commonlength:]) + diffs[pointer].Text
						text_insert = text_insert[:len(text_insert)-commonlength]
						text_delete = text_delete[:len(text_delete)-commonlength]
					}
				}
				// Insert the merged records.
				if len(text_delete) == 0 {
					diffs = append(diffs[:pointer-count_insert], append([]Diff{Diff{INSERT, string(text_insert)}}, diffs[pointer:]...)...)
				} else if len(text_insert) == 0 {
					diffs = append(diffs[:pointer-count_delete], append([]Diff{Diff{DELETE, string(text_delete)}}, diffs[pointer:]...)...)
				} else {
					diffs = append(diffs[:pointer-count_delete-count_insert],
						append([]Diff{Diff{INSERT, string(text_insert)}, Diff{DELETE, string(text_delete)}}, diffs[pointer:]...)...)
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
			text_delete = []rune{}
			text_insert = []rune{}
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
		_, diffs = dmp.DiffCleanupMerge(diffs)
	}
	return nil, diffs
}

// * diff_prettyHtml
// Convert a diff array into a pretty HTML report.
func DiffPrettyHtml(diffs []Diff) string {
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
