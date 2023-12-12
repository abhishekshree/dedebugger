package debugger

// NewDebugger initializes a new Debugger instance.
func NewDebugger() *Debugger {
	return &Debugger{
		BreakpointSet: false,
		InterruptCode: []byte{0xCC},
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
