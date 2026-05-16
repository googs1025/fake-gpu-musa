package common

type GPU struct {
	Idx      uint
	Name     string
	UUID     string
	TotalMem uint64
	UsedMem  uint64
	Util     uint
	TempC    int
	PowerMW  uint
}
