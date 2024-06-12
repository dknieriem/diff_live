package main

import (
	"fmt"
	"syscall/js"

	"github.com/dknieriem/diff_live/cmd/wasm/diff"
)

// * Go:
// * diff_main needs text1, text2, opt_checklines (default true), opt_deadline (default to 1 sec)

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
		dmp := new(diff.DiffMatchPatch) //.New()
		err, diffs := dmp.DiffRecurse(inputA, inputB)
		if err != nil {
			errStr := fmt.Sprintf("unable to parse JSON. Error %s occurred\n", err)
			result := map[string]any{
				"error": errStr,
			}
			return result
		}
		diffs = dmp.DiffCleanupSemantic(diffs)
		diffs = dmp.DiffCleanupEfficiency(diffs)
		htmlDiff := diff.DiffPrettyHtml(diffs)
		DiffResultArea.Set("value", htmlDiff)
		return nil
	})
	return diffFunc
}

func main() {
	fmt.Println("Go Web Assembly")
	js.Global().Set("diffStrings", diffWrapper())
	<-make(chan struct{})
}
