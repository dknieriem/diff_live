package main

const (
	DELETE Operation = iota + 1
	INSERT
	EQUAL
)

type Operation int

func (op Operation) String() string {
	return [...]string{"DELETE", "INSERT", "EQUAL"}[op-1]
}

func (op Operation) EnumIndex() int {
	return int(op)
}
