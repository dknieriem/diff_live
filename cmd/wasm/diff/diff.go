package diff

import "strings"

type Diff struct {
	Type Operation
	Text string
}

func (diff *Diff) toString() string {
	outStr := strings.ReplaceAll(diff.Text, "\n", "\u00b6")
	outStr = "Diff(" + diff.Type.String() + ",\"" + outStr + "\")"
	return outStr
}
