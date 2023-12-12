package debugger

import (
	"debug/gosym"
	"syscall"
)

// Debugger holds the state of the debugger.
type Debugger struct {
	TargetFile    string
	Line          int
	PC            uint64
	Fn            *gosym.Func
	SymTable      *gosym.Table
	Regs          syscall.PtraceRegs
	Ws            syscall.WaitStatus
	OriginalCode  []byte
	BreakpointSet bool
	InterruptCode []byte

	DebuggerInterface
}

type DebuggerInterface interface {
	InputOrContinue(pid int) bool
	SetBreak(pid int) (bool, []byte)
	ReplaceCode(pid int, address uint64, code []byte) []byte
	GetSymbolTable(prog string) *gosym.Table
	OutputStack(pid int, ip uint64, sp uint64, bp uint64)
	RunTarget(target string)
	Run()
}
