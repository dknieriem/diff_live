package main

import (
	"fmt"
	"syscall/js"
)

type Operation int

const (
	DELETE Operation = iota + 1
	INSERT
	EQUAL
)

func (op Operation) String() string {
	return [...]string{"DELETE", "INSERT", "EQUAL"}[op-1]
}

func (op Operation) EnumIndex() int {
	return int(op)
}

type Diff struct {
}

// The main diff method, returning
func diffMain(inputA, inputB string) (string, error) {
	return diffMain(inputA, inputB, true)
}

func diffMain(inputA, inputB string, checklines bool) {

}

// * Go:
// * diff_main needs text1, text2, opt_checklines (default true), opt_deadline (default to 1 sec)
// * diff_compute_
// * diff_commonPrefix
// * diff_commonSuffix
// * diff_cleanupMerge
// * diff_halfMatch_
// * diff_lineMode_
// * diff_bisect_
// * new Diff()
// * diff_cleanupSemantic
// * diff_prettyHtml
// Convert a diff array into a pretty HTML report.
func diff_prettyHtml(diffs []Diff) {
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
	// return html.join('');
}

// * diff_cleanupSemanticLossless

// Added function to wrap the diff call for js exposure
func diffWrapper() js.Func {
	diffFunc := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) != 2 {
			result := map[string]any{
				"error": "Invalid no. of arguments passed - 2 required",
			}
			return result
		}
		jsDoc := js.Global().Get("document")
		if !jsDoc.Truthy() {
			result := map[string]any{
				"error": "Unable to get document object",
			}
			return result
		}
		DiffResultArea := jsDoc.Call("getElementById", "diffoutput")
		if !DiffResultArea.Truthy() {
			result := map[string]any{
				"error": "Unable to get output text area #diffoutput",
			}
			return result
		}
		inputA := args[0].String()
		inputB := args[1].String()
		fmt.Printf("inputA %s\n", inputA)
		fmt.Printf("inputB %s\n", inputB)
		diffs, err := diffMain(inputA, inputB)
		if err != nil {
			errStr := fmt.Sprintf("unable to parse JSON. Error %s occurred\n", err)
			result := map[string]any{
				"error": errStr,
			}
			return result
		}
		diffs = diff_cleanupSematic(diffs)
		diffs = diff_cleanupEfficiency(diffs)
		htmlDiff := diffPrettyHtml(diffs)
		DiffResultArea.Set("value", htmlDiff)
		return nile
	})
	return diffFunc
}

func main() {
	fmt.Println("Go Web Assembly")
	js.Global().Set("diffStrings", diffWrapper())
	<-make(chan struct{})
}
