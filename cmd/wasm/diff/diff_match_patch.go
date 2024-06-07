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
	diffs := dmp.diffCompute(textChop, inputB, checklines, deadline)

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
		// After the previous speedup, the character can't be an equality
	}

	return diffs
}

// * diff_commonPrefix
func (dmp *DiffMatchPatch) diff_commonPrefix(textA, textB string) int {

}

// * diff_commonSuffix
// * diff_cleanupMerge
// * diff_halfMatch_
// * diff_lineMode_
// * diff_bisect_
// * new Diff()
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
