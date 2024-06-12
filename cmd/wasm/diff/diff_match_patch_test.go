package diff

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestDiffCommonPrefix(t *testing.T) {
	type TestCase struct {
		Name string

		TextA string
		TextB string

		Expected int
	}

	dmp := New()

	for i, tc := range []TestCase{
		{"Null", "abc", "xyz", 0},
		{"Non-null", "1234abcdef", "1234xyz", 4},
		{"Whole", "1234", "1234xyz", 4},
	} {
		actual := dmp.DiffCommonPrefix([]rune(tc.TextA), []rune(tc.TextB))
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

func TestDiffCommonSuffix(t *testing.T) {
	type TestCase struct {
		Name string

		TextA string
		TextB string

		Expected int
	}

	dmp := New()

	for i, tc := range []TestCase{
		{"Null", "abc", "xyz", 0},
		{"Non-null", "abcdef1234", "xyz1234", 4},
		{"Whole", "1234", "xyz1234", 4},
	} {
		actual := dmp.DiffCommonSuffix([]rune(tc.TextA), []rune(tc.TextB))
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

func TestDiffCommonOverlap(t *testing.T) {
	type TestCase struct {
		Name string

		TextA string
		TextB string

		Expected int
	}

	dmp := New()

	for i, tc := range []TestCase{
		{"Null", "", "abcd", 0},
		{"Whole", "abc", "abcd", 3},
		{"Null", "123456", "abcd", 0},
		{"Null", "123456xxx", "xxxabcd", 3},
		{"Unicode", "fi", "\ufb01i", 0},
	} {
		actual := dmp.DiffCommonOverlap(tc.TextA, tc.TextB)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

// TODO fix halfmatch method. Broken!
func TestDiffHalfMatch(t *testing.T) {
	type TestCase struct {
		TextA string
		TextB string

		Expected []string
	}

	dmp := New()
	dmp.Diff_Timeout = 1

	for i, tc := range []TestCase{
		// No match
		{"1234567890", "abcdef", []string{"", "", "", "", ""}},
		{"12345", "23", []string{"", "", "", "", ""}},

		// Single Match
		{"1234567890", "a345678z", []string{"12", "90", "a", "z", "345678"}},
		{"a345678z", "1234567890", []string{"a", "z", "12", "90", "345678"}},
		{"abc56789z", "1234567890", []string{"abc", "z", "1234", "0", "56789"}},
		{"a23456xyz", "1234567890", []string{"a", "xyz", "1", "7890", "23456"}},

		// Multiple Matches
		{"121231234123451234123121", "a1234123451234z", []string{"12123", "123121", "a", "z", "1234123451234"}},
		{"x-=-=-=-=-=-=-=-=-=-=-=-=", "xx-=-=-=-=-=-=-=", []string{"", "-=-=-=-=-=", "x", "", "x-=-=-=-=-=-=-="}},
		{"-=-=-=-=-=-=-=-=-=-=-=-=y", "-=-=-=-=-=-=-=yy", []string{"-=-=-=-=-=", "", "", "y", "-=-=-=-=-=-=-=y"}},

		// Non-optimal halfmatch, ptimal diff would be -q+x=H-i+e=lloHe+Hu=llo-Hew+y not -qHillo+x=HelloHe-w+Hulloy
		{"qHilloHelloHew", "xHelloHeHulloy", []string{"qHillo", "w", "x", "Hulloy", "HelloHe"}},
	} {
		actual1, actual2, actual3, actual4, actual5 := dmp.DiffHalfMatch([]rune(tc.TextA), []rune(tc.TextB))
		actual := []string{string(actual1), string(actual2), string(actual3), string(actual4), string(actual5)}
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %#v", i, tc))
	}

	dmp.Diff_Timeout = 0

	for i, tc := range []TestCase{
		// Optimal no halfmatch
		{"qHilloHelloHew", "xHelloHeHulloy", []string{"", "", "", "", ""}},
	} {
		actual1, actual2, actual3, actual4, actual5 := dmp.DiffHalfMatch([]rune(tc.TextA), []rune(tc.TextB))
		actual := []string{string(actual1), string(actual2), string(actual3), string(actual4), string(actual5)}
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %#v", i, tc))
	}
}

func TestDiffBisectSplit(t *testing.T) {
	type TestCase struct {
		TextA string
		TextB string
	}

	dmp := New()

	for _, tc := range []TestCase{
		{"STUV\x05WX\x05YZ\x05[", "WĺĻļ\x05YZ\x05ĽľĿŀZ"},
	} {
		diffs := dmp.DiffBisectSplit([]rune(tc.TextA),
			[]rune(tc.TextB), 7, 6, time.Now().Add(time.Hour))

		for _, d := range diffs {
			assert.True(t, utf8.ValidString(d.Text))
		}
	}
}

// TODO: fix. panic: runtime error: slice bounds out of range [7:6] from (*DiffMatchPatch).DiffLinesToCharsMunge
func TestDiffLinesToChars(t *testing.T) {
	type TestCase struct {
		TextA string
		TextB string

		ExpectedChars1 string
		ExpectedChars2 string
		ExpectedLines  []string
	}

	dmp := New()

	for i, tc := range []TestCase{
		{"", "alpha\r\nbeta\r\n\r\n\r\n", "", "\x01\x02\x03\x03", []string{"", "alpha\r\n", "beta\r\n", "\r\n"}},
		{"a", "b", "\x01", "\x02", []string{"", "a", "b"}},
		// Omit final newline.
		{"alpha\nbeta\nalpha", "", "\x01\x02\x03", "", []string{"", "alpha\n", "beta\n", "alpha"}},
		// Same lines in TextA and TextB
		{"abc\ndefg\n12345\n", "abc\ndef\n12345\n678", "\x01\x02\x03", "\x01\x04\x03\x05", []string{"", "abc\n", "defg\n", "12345\n", "def\n", "678"}},
	} {
		actualChars1, actualChars2, actualLines := dmp.DiffLinesToChars(tc.TextA, tc.TextB)
		assert.Equal(t, tc.ExpectedChars1, actualChars1, fmt.Sprintf("Test case #%d, %#v", i, tc))
		assert.Equal(t, tc.ExpectedChars2, actualChars2, fmt.Sprintf("Test case #%d, %#v", i, tc))
		assert.Equal(t, tc.ExpectedLines, actualLines, fmt.Sprintf("Test case #%d, %#v", i, tc))
	}

	// More than 256 to reveal any 8-bit limitations.
	n := 300
	lineList := []string{
		"", // Account for the initial empty element of the lines array.
	}
	var charList []rune
	for x := 1; x < n+1; x++ {
		lineList = append(lineList, strconv.Itoa(x)+"\n")
		charList = append(charList, rune(x))
	}
	lines := strings.Join(lineList, "")
	chars := string(charList)

	actualChars1, actualChars2, actualLines := dmp.DiffLinesToChars(lines, "")
	assert.Equal(t, chars, actualChars1)
	assert.Equal(t, "", actualChars2)
	assert.Equal(t, lineList, actualLines)
}

// TODO: fix. DELETE diff error - length should be 300 runes / 1092 chars, gave 172 runes / 1784 chars
func TestDiffCharsToLines(t *testing.T) {
	type TestCase struct {
		Diffs []Diff
		Lines []string

		Expected []Diff
	}

	dmp := New()

	for i, tc := range []TestCase{
		{
			Diffs: []Diff{
				{EQUAL, "\x01\x02\x01"},
				{INSERT, "\x02\x01\x02"},
			},
			Lines: []string{"", "alpha\n", "beta\n"},

			Expected: []Diff{
				{EQUAL, "alpha\nbeta\nalpha\n"},
				{INSERT, "beta\nalpha\nbeta\n"},
			},
		},
	} {
		actual := dmp.DiffCharsToLines(tc.Diffs, tc.Lines)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %#v", i, tc))
	}

	// More than 256 to reveal any 8-bit limitations.
	n := 300
	lineList := []string{
		"", // Account for the initial empty element of the lines array.
	}
	charList := []rune{}
	for x := 1; x <= n; x++ {
		lineList = append(lineList, strconv.Itoa(x)+"\n")
		charList = append(charList, rune(x))
	}
	chars := string(charList)

	actual := dmp.DiffCharsToLines([]Diff{Diff{DELETE, chars}}, lineList)
	assert.Equal(t, []Diff{Diff{DELETE, strings.Join(lineList, "")}}, actual)
}

// TODO: fix!
// 	Not equal: (Type:2 === INSERT)
// expected: []diff.Diff{diff.Diff{Type:3, Text:"a"}, diff.Diff{Type:1, Text:"b"}, diff.Diff{Type:2, Text:"c"}}
// actual  : []diff.Diff{diff.Diff{Type:3, Text:"a"}, diff.Diff{Type:1, Text:"b"}, diff.Diff{Type:2, Text:""}, diff.Diff{Type:3, Text:"c"}}

func TestDiffCleanupMerge(t *testing.T) {
	type TestCase struct {
		Name string

		Diffs []Diff

		Expected []Diff
	}

	dmp := New()

	for i, tc := range []TestCase{
		{
			"Null case",
			[]Diff{},
			[]Diff{},
		},
		{
			"No Diff case",
			[]Diff{Diff{EQUAL, "a"}, Diff{DELETE, "b"}, Diff{INSERT, "c"}},
			[]Diff{Diff{EQUAL, "a"}, Diff{DELETE, "b"}, Diff{INSERT, "c"}},
		},
		{
			"Merge equalities",
			[]Diff{Diff{EQUAL, "a"}, Diff{EQUAL, "b"}, Diff{EQUAL, "c"}},
			[]Diff{Diff{EQUAL, "abc"}},
		},
		{
			"Merge deletions",
			[]Diff{Diff{DELETE, "a"}, Diff{DELETE, "b"}, Diff{DELETE, "c"}},
			[]Diff{Diff{DELETE, "abc"}},
		},
		{
			"Merge insertions",
			[]Diff{Diff{INSERT, "a"}, Diff{INSERT, "b"}, Diff{INSERT, "c"}},
			[]Diff{Diff{INSERT, "abc"}},
		},
		{
			"Merge interweave",
			[]Diff{Diff{DELETE, "a"}, Diff{INSERT, "b"}, Diff{DELETE, "c"}, Diff{INSERT, "d"}, Diff{EQUAL, "e"}, Diff{EQUAL, "f"}},
			[]Diff{Diff{DELETE, "ac"}, Diff{INSERT, "bd"}, Diff{EQUAL, "ef"}},
		},
		{
			"Prefix and suffix detection",
			[]Diff{Diff{DELETE, "a"}, Diff{INSERT, "abc"}, Diff{DELETE, "dc"}},
			[]Diff{Diff{EQUAL, "a"}, Diff{DELETE, "d"}, Diff{INSERT, "b"}, Diff{EQUAL, "c"}},
		},
		{
			"Prefix and suffix detection with equalities",
			[]Diff{Diff{EQUAL, "x"}, Diff{DELETE, "a"}, Diff{INSERT, "abc"}, Diff{DELETE, "dc"}, Diff{EQUAL, "y"}},
			[]Diff{Diff{EQUAL, "xa"}, Diff{DELETE, "d"}, Diff{INSERT, "b"}, Diff{EQUAL, "cy"}},
		},
		{
			"Same test as above but with unicode (\u0101 will appear in diffs with at least 257 unique lines)",
			[]Diff{Diff{EQUAL, "x"}, Diff{DELETE, "\u0101"}, Diff{INSERT, "\u0101bc"}, Diff{DELETE, "dc"}, Diff{EQUAL, "y"}},
			[]Diff{Diff{EQUAL, "x\u0101"}, Diff{DELETE, "d"}, Diff{INSERT, "b"}, Diff{EQUAL, "cy"}},
		},
		{
			"Slide edit left",
			[]Diff{Diff{EQUAL, "a"}, Diff{INSERT, "ba"}, Diff{EQUAL, "c"}},
			[]Diff{Diff{INSERT, "ab"}, Diff{EQUAL, "ac"}},
		},
		{
			"Slide edit right",
			[]Diff{Diff{EQUAL, "c"}, Diff{INSERT, "ab"}, Diff{EQUAL, "a"}},
			[]Diff{Diff{EQUAL, "ca"}, Diff{INSERT, "ba"}},
		},
		{
			"Slide edit left recursive",
			[]Diff{Diff{EQUAL, "a"}, Diff{DELETE, "b"}, Diff{EQUAL, "c"}, Diff{DELETE, "ac"}, Diff{EQUAL, "x"}},
			[]Diff{Diff{DELETE, "abc"}, Diff{EQUAL, "acx"}},
		},
		{
			"Slide edit right recursive",
			[]Diff{Diff{EQUAL, "x"}, Diff{DELETE, "ca"}, Diff{EQUAL, "c"}, Diff{DELETE, "b"}, Diff{EQUAL, "a"}},
			[]Diff{Diff{EQUAL, "xca"}, Diff{DELETE, "cba"}},
		},
	} {
		_, actual := dmp.DiffCleanupMerge(tc.Diffs)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

// TODO: fix
// Not equal:
// expected: []diff.Diff{diff.Diff{Type:3, Text:"The "}, diff.Diff{Type:2, Text:"cow and the "}, diff.Diff{Type:3, Text:"cat."}}
// actual  : []diff.Diff{diff.Diff{Type:2, Text:"the cow and "}, diff.Diff{Type:3, Text:"the cat."}}
func TestDiffCleanupSemanticLossless(t *testing.T) {
	type TestCase struct {
		Name string

		Diffs []Diff

		Expected []Diff
	}

	dmp := New()

	for i, tc := range []TestCase{
		{
			"Null case",
			[]Diff{},
			[]Diff{},
		},
		{
			"Blank lines",
			[]Diff{
				Diff{EQUAL, "AAA\r\n\r\nBBB"},
				Diff{INSERT, "\r\nDDD\r\n\r\nBBB"},
				Diff{EQUAL, "\r\nEEE"},
			},
			[]Diff{
				Diff{EQUAL, "AAA\r\n\r\n"},
				Diff{INSERT, "BBB\r\nDDD\r\n\r\n"},
				Diff{EQUAL, "BBB\r\nEEE"},
			},
		},
		{
			"Line boundaries",
			[]Diff{
				Diff{EQUAL, "AAA\r\nBBB"},
				Diff{INSERT, " DDD\r\nBBB"},
				Diff{EQUAL, " EEE"},
			},
			[]Diff{
				Diff{EQUAL, "AAA\r\n"},
				Diff{INSERT, "BBB DDD\r\n"},
				Diff{EQUAL, "BBB EEE"},
			},
		},
		{
			"Word boundaries",
			[]Diff{
				Diff{EQUAL, "The c"},
				Diff{INSERT, "ow and the c"},
				Diff{EQUAL, "at."},
			},
			[]Diff{
				Diff{EQUAL, "The "},
				Diff{INSERT, "cow and the "},
				Diff{EQUAL, "cat."},
			},
		},
		{
			"Alphanumeric boundaries",
			[]Diff{
				Diff{EQUAL, "The-c"},
				Diff{INSERT, "ow-and-the-c"},
				Diff{EQUAL, "at."},
			},
			[]Diff{
				Diff{EQUAL, "The-"},
				Diff{INSERT, "cow-and-the-"},
				Diff{EQUAL, "cat."},
			},
		},
		{
			"Hitting the start",
			[]Diff{
				Diff{EQUAL, "a"},
				Diff{DELETE, "a"},
				Diff{EQUAL, "ax"},
			},
			[]Diff{
				Diff{DELETE, "a"},
				Diff{EQUAL, "aax"},
			},
		},
		{
			"Hitting the end",
			[]Diff{
				Diff{EQUAL, "xa"},
				Diff{DELETE, "a"},
				Diff{EQUAL, "a"},
			},
			[]Diff{
				Diff{EQUAL, "xaa"},
				Diff{DELETE, "a"},
			},
		},
		{
			"Sentence boundaries",
			[]Diff{
				Diff{EQUAL, "The xxx. The "},
				Diff{INSERT, "zzz. The "},
				Diff{EQUAL, "yyy."},
			},
			[]Diff{
				Diff{EQUAL, "The xxx."},
				Diff{INSERT, " The zzz."},
				Diff{EQUAL, " The yyy."},
			},
		},
		{
			"UTF-8 strings",
			[]Diff{
				Diff{EQUAL, "The ♕. The "},
				Diff{INSERT, "♔. The "},
				Diff{EQUAL, "♖."},
			},
			[]Diff{
				Diff{EQUAL, "The ♕."},
				Diff{INSERT, " The ♔."},
				Diff{EQUAL, " The ♖."},
			},
		},
		{
			"Rune boundaries",
			[]Diff{
				Diff{EQUAL, "♕♕"},
				Diff{INSERT, "♔♔"},
				Diff{EQUAL, "♖♖"},
			},
			[]Diff{
				Diff{EQUAL, "♕♕"},
				Diff{INSERT, "♔♔"},
				Diff{EQUAL, "♖♖"},
			},
		},
	} {
		actual := dmp.DiffCleanupSemanticLossless(tc.Diffs)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

// TODO: fix
// Not equal:
// expected: []diff.Diff{diff.Diff{Type:3, Text:"2016-09-01T03:07:1"}, diff.Diff{Type:2, Text:"5.15"}, diff.Diff{Type:3, Text:"4"}, diff.Diff{Type:1, Text:"."}, diff.Diff{Type:3, Text:"80"}, diff.Diff{Type:2, Text:"0"}, diff.Diff{Type:3, Text:"78"}, diff.Diff{Type:1, Text:"3074"}, diff.Diff{Type:3, Text:"1Z"}}
// actual  : []diff.Diff{diff.Diff{Type:3, Text:"2016-09-01T03:07:1"}, diff.Diff{Type:2, Text:"5.158001Z"}, diff.Diff{Type:1, Text:"4.783074"}}
func TestDiffCleanupSemantic(t *testing.T) {
	type TestCase struct {
		Name string

		Diffs []Diff

		Expected []Diff
	}

	dmp := New()

	for i, tc := range []TestCase{
		{
			"Null case",
			[]Diff{},
			[]Diff{},
		},
		{
			"No elimination #1",
			[]Diff{
				{DELETE, "ab"},
				{INSERT, "cd"},
				{EQUAL, "12"},
				{DELETE, "e"},
			},
			[]Diff{
				{DELETE, "ab"},
				{INSERT, "cd"},
				{EQUAL, "12"},
				{DELETE, "e"},
			},
		},
		{
			"No elimination #2",
			[]Diff{
				{DELETE, "abc"},
				{INSERT, "ABC"},
				{EQUAL, "1234"},
				{DELETE, "wxyz"},
			},
			[]Diff{
				{DELETE, "abc"},
				{INSERT, "ABC"},
				{EQUAL, "1234"},
				{DELETE, "wxyz"},
			},
		},
		{
			"No elimination #3",
			[]Diff{
				{EQUAL, "2016-09-01T03:07:1"},
				{INSERT, "5.15"},
				{EQUAL, "4"},
				{DELETE, "."},
				{EQUAL, "80"},
				{INSERT, "0"},
				{EQUAL, "78"},
				{DELETE, "3074"},
				{EQUAL, "1Z"},
			},
			[]Diff{
				{EQUAL, "2016-09-01T03:07:1"},
				{INSERT, "5.15"},
				{EQUAL, "4"},
				{DELETE, "."},
				{EQUAL, "80"},
				{INSERT, "0"},
				{EQUAL, "78"},
				{DELETE, "3074"},
				{EQUAL, "1Z"},
			},
		},
		{
			"Simple elimination",
			[]Diff{
				{DELETE, "a"},
				{EQUAL, "b"},
				{DELETE, "c"},
			},
			[]Diff{
				{DELETE, "abc"},
				{INSERT, "b"},
			},
		},
		{
			"Backpass elimination",
			[]Diff{
				{DELETE, "ab"},
				{EQUAL, "cd"},
				{DELETE, "e"},
				{EQUAL, "f"},
				{INSERT, "g"},
			},
			[]Diff{
				{DELETE, "abcdef"},
				{INSERT, "cdfg"},
			},
		},
		{
			"Multiple eliminations",
			[]Diff{
				{INSERT, "1"},
				{EQUAL, "A"},
				{DELETE, "B"},
				{INSERT, "2"},
				{EQUAL, "_"},
				{INSERT, "1"},
				{EQUAL, "A"},
				{DELETE, "B"},
				{INSERT, "2"},
			},
			[]Diff{
				{DELETE, "AB_AB"},
				{INSERT, "1A2_1A2"},
			},
		},
		{
			"Word boundaries",
			[]Diff{
				{EQUAL, "The c"},
				{DELETE, "ow and the c"},
				{EQUAL, "at."},
			},
			[]Diff{
				{EQUAL, "The "},
				{DELETE, "cow and the "},
				{EQUAL, "cat."},
			},
		},
		{
			"No overlap elimination",
			[]Diff{
				{DELETE, "abcxx"},
				{INSERT, "xxdef"},
			},
			[]Diff{
				{DELETE, "abcxx"},
				{INSERT, "xxdef"},
			},
		},
		{
			"Overlap elimination",
			[]Diff{
				{DELETE, "abcxxx"},
				{INSERT, "xxxdef"},
			},
			[]Diff{
				{DELETE, "abc"},
				{EQUAL, "xxx"},
				{INSERT, "def"},
			},
		},
		{
			"Reverse overlap elimination",
			[]Diff{
				{DELETE, "xxxabc"},
				{INSERT, "defxxx"},
			},
			[]Diff{
				{INSERT, "def"},
				{EQUAL, "xxx"},
				{DELETE, "abc"},
			},
		},
		{
			"Two overlap eliminations",
			[]Diff{
				{DELETE, "abcd1212"},
				{INSERT, "1212efghi"},
				{EQUAL, "----"},
				{DELETE, "A3"},
				{INSERT, "3BC"},
			},
			[]Diff{
				{DELETE, "abcd"},
				{EQUAL, "1212"},
				{INSERT, "efghi"},
				{EQUAL, "----"},
				{DELETE, "A"},
				{EQUAL, "3"},
				{INSERT, "BC"},
			},
		},
		{
			"Test case for adapting DiffCleanupSemantic to be equal to the Python version #19",
			[]Diff{
				{EQUAL, "James McCarthy "},
				{DELETE, "close to "},
				{EQUAL, "sign"},
				{DELETE, "ing"},
				{INSERT, "s"},
				{EQUAL, " new "},
				{DELETE, "E"},
				{INSERT, "fi"},
				{EQUAL, "ve"},
				{INSERT, "-yea"},
				{EQUAL, "r"},
				{DELETE, "ton"},
				{EQUAL, " deal"},
				{INSERT, " at Everton"},
			},
			[]Diff{
				{EQUAL, "James McCarthy "},
				{DELETE, "close to "},
				{EQUAL, "sign"},
				{DELETE, "ing"},
				{INSERT, "s"},
				{EQUAL, " new "},
				{INSERT, "five-year deal at "},
				{EQUAL, "Everton"},
				{DELETE, " deal"},
			},
		},
		{
			"Taken from python / CPP library",
			[]Diff{
				{INSERT, "星球大戰：新的希望 "},
				{EQUAL, "star wars: "},
				{DELETE, "episodio iv - un"},
				{EQUAL, "a n"},
				{DELETE, "u"},
				{EQUAL, "e"},
				{DELETE, "va"},
				{INSERT, "w"},
				{EQUAL, " "},
				{DELETE, "es"},
				{INSERT, "ho"},
				{EQUAL, "pe"},
				{DELETE, "ranza"},
			},
			[]Diff{
				{INSERT, "星球大戰：新的希望 "},
				{EQUAL, "star wars: "},
				{DELETE, "episodio iv - una nueva esperanza"},
				{INSERT, "a new hope"},
			},
		},
		{
			"panic",
			[]Diff{
				{INSERT, "킬러 인 "},
				{EQUAL, "리커버리"},
				{DELETE, " 보이즈"},
			},
			[]Diff{
				{INSERT, "킬러 인 "},
				{EQUAL, "리커버리"},
				{DELETE, " 보이즈"},
			},
		},
	} {
		actual := dmp.DiffCleanupSemantic(tc.Diffs)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

// TODO: fix
// Not equal:
// expected: []diff.Diff{diff.Diff{Type:1, Text:"abxyzcd"}, diff.Diff{Type:2, Text:"12xyz34"}}
// actual  : []diff.Diff{diff.Diff{Type:2, Text:"12xyz34"}, diff.Diff{Type:1, Text:"abxyzcd"}}
func TestDiffCleanupEfficiency(t *testing.T) {
	type TestCase struct {
		Name string

		Diffs []Diff

		Expected []Diff
	}

	dmp := New()
	dmp.Diff_EditCost = 4

	for i, tc := range []TestCase{
		{
			"Null case",
			[]Diff{},
			[]Diff{},
		},
		{
			"No elimination",
			[]Diff{
				Diff{DELETE, "ab"},
				Diff{INSERT, "12"},
				Diff{EQUAL, "wxyz"},
				Diff{DELETE, "cd"},
				Diff{INSERT, "34"},
			},
			[]Diff{
				Diff{DELETE, "ab"},
				Diff{INSERT, "12"},
				Diff{EQUAL, "wxyz"},
				Diff{DELETE, "cd"},
				Diff{INSERT, "34"},
			},
		},
		{
			"Four-edit elimination",
			[]Diff{
				Diff{DELETE, "ab"},
				Diff{INSERT, "12"},
				Diff{EQUAL, "xyz"},
				Diff{DELETE, "cd"},
				Diff{INSERT, "34"},
			},
			[]Diff{
				Diff{DELETE, "abxyzcd"},
				Diff{INSERT, "12xyz34"},
			},
		},
		{
			"Three-edit elimination",
			[]Diff{
				Diff{INSERT, "12"},
				Diff{EQUAL, "x"},
				Diff{DELETE, "cd"},
				Diff{INSERT, "34"},
			},
			[]Diff{
				Diff{DELETE, "xcd"},
				Diff{INSERT, "12x34"},
			},
		},
		{
			"Backpass elimination",
			[]Diff{
				Diff{DELETE, "ab"},
				Diff{INSERT, "12"},
				Diff{EQUAL, "xy"},
				Diff{INSERT, "34"},
				Diff{EQUAL, "z"},
				Diff{DELETE, "cd"},
				Diff{INSERT, "56"},
			},
			[]Diff{
				Diff{DELETE, "abxyzcd"},
				Diff{INSERT, "12xy34z56"},
			},
		},
	} {
		actual := dmp.DiffCleanupEfficiency(tc.Diffs)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}

	dmp.Diff_EditCost = 5

	for i, tc := range []TestCase{
		{
			"High cost elimination",
			[]Diff{
				Diff{DELETE, "ab"},
				Diff{INSERT, "12"},
				Diff{EQUAL, "wxyz"},
				Diff{DELETE, "cd"},
				Diff{INSERT, "34"},
			},
			[]Diff{
				Diff{DELETE, "abwxyzcd"},
				Diff{INSERT, "12wxyz34"},
			},
		},
	} {
		actual := dmp.DiffCleanupEfficiency(tc.Diffs)
		assert.Equal(t, tc.Expected, actual, fmt.Sprintf("Test case #%d, %s", i, tc.Name))
	}
}

func TestDiffText(t *testing.T) {
	type TestCase struct {
		Diffs []Diff

		ExpectedText1 string
		ExpectedText2 string
	}

	dmp := New()

	for i, tc := range []TestCase{
		{
			Diffs: []Diff{
				{EQUAL, "jump"},
				{DELETE, "s"},
				{INSERT, "ed"},
				{EQUAL, " over "},
				{DELETE, "the"},
				{INSERT, "a"},
				{EQUAL, " lazy"},
			},

			ExpectedText1: "jumps over the lazy",
			ExpectedText2: "jumped over a lazy",
		},
	} {
		actualText1 := dmp.DiffTextSource(tc.Diffs)
		assert.Equal(t, tc.ExpectedText1, actualText1, fmt.Sprintf("Test case #%d, %#v", i, tc))

		actualText2 := dmp.DiffTextResult(tc.Diffs)
		assert.Equal(t, tc.ExpectedText2, actualText2, fmt.Sprintf("Test case #%d, %#v", i, tc))
	}
}
