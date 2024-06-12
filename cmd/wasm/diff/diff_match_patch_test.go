package diff

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffPrettyHtml(t *testing.T) {
	type TestCase struct {
		Diffs []Diff

		Expected string
	}

	for i, tc := range []TestCase{
		{
			Diffs: []Diff{
				{EQUAL, "a\n"},
				{DELETE, "<B>b</B>"},
				{INSERT, "c&d"},
			},

			Expected: "<span>a&para;<br></span><del style=\"background:#ffe6e6;\">&lt;B&gt;b&lt;/B&gt;</del><ins style=\"background:#e6ffe6;\">c&amp;d</ins>",
		},
	} {
		actual := DiffPrettyHtml(tc.Diffs)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %#v", i, tc))
	}
}
