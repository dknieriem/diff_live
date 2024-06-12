package diff

import (
	"bytes"
	"strings"
)

type Diff struct {
	Type Operation
	Text string
}

func (diff *Diff) toString() string {
	outStr := strings.ReplaceAll(diff.Text, "\n", "\u00b6")
	outStr = "Diff(" + diff.Type.String() + ",\"" + outStr + "\")"
	return outStr
}

func (dmp *DiffMatchPatch) DiffTextSource(diffs []Diff) string {
	//StringBuilder text = new StringBuilder()
	var text bytes.Buffer

	for _, aDiff := range diffs {
		if aDiff.Type != INSERT {
			_, _ = text.WriteString(aDiff.Text)
		}
	}
	return text.String()
}

func (dmp *DiffMatchPatch) DiffTextResult(diffs []Diff) string {
	var text bytes.Buffer

	for _, aDiff := range diffs {
		if aDiff.Type != DELETE {
			_, _ = text.WriteString(aDiff.Text)
		}
	}
	return text.String()
}
