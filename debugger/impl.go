package debugger

import (
	"bufio"
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// InputOrContinue gets user input to determine whether to continue, step, set a breakpoint, or quit.
func (d *Debugger) InputOrContinue(pid int) bool {
	sub := false
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("\n(C)ontinue, (S)tep, set (B)reakpoint or (Q)uit? > ")
	for {
		scanner.Scan()
		input := scanner.Text()
		switch strings.ToUpper(input) {
		case "C":
			return true
		case "S":
			return false
		case "B":
			fmt.Printf("  Enter line number in %s: > ", d.TargetFile)
			sub = true
		case "Q":
			os.Exit(0)
		default:
			if sub {
				d.Line, _ = strconv.Atoi(input)
				d.BreakpointSet, d.OriginalCode = d.SetBreak(pid)
				return true
			}
			fmt.Printf("Unexpected input %s\n", input)
			fmt.Printf("\n(C)ontinue, (S)tep, set (B)reakpoint or (Q)uit? > ")
		}
	}
}

// SetBreak sets a breakpoint at the specified line.
func (d *Debugger) SetBreak(pid int) (bool, []byte) {
	var err error
	d.PC, _, err = d.SymTable.LineToPC(d.TargetFile, d.Line)
	if err != nil {
		fmt.Printf("Can't find breakpoint for %s, %d\n", d.TargetFile, d.Line)
		return false, []byte{}
	}

	return true, d.ReplaceCode(pid, d.PC, d.InterruptCode)
}

// ReplaceCode replaces the code at the specified address with new code.
func (d *Debugger) ReplaceCode(pid int, address uint64, code []byte) []byte {
	original := make([]byte, len(code))
	syscall.PtracePeekData(pid, uintptr(address), original)
	syscall.PtracePokeData(pid, uintptr(address), code)
	return original
}

// GetSymbolTable retrieves the symbol table from the specified executable.
func (d *Debugger) GetSymbolTable(prog string) *gosym.Table {
	exe, err := elf.Open(prog)
	must(err)
	defer exe.Close()

	addr := exe.Section(".text").Addr

	lineTableData, err := exe.Section(".gopclntab").Data()
	must(err)

	lineTable := gosym.NewLineTable(lineTableData, addr)
	must(err)

	symTableData, err := exe.Section(".gosymtab").Data()
	must(err)

	symTable, err := gosym.NewTable(symTableData, lineTable)
	must(err)

	return symTable
}

// OutputStack outputs the call stack information.
func (d *Debugger) OutputStack(pid int, ip uint64, sp uint64, bp uint64) {
	_, _, d.Fn = d.SymTable.PCToLine(ip)

	var i uint64
	var nextbp uint64

	for {
		i = 0
		frameSize := bp - sp + 8

		// If we look at bp / sp while they are being updated we can
		// get some odd results
		if frameSize > 1000 || bp == 0 {
			fmt.Printf("Strange frame size: SP: %X | BP : %X \n", sp, bp)
			frameSize = 32
			bp = sp + frameSize - 8
		}

		// Read the next stack frame
		b := make([]byte, frameSize)
		_, err := syscall.PtracePeekData(pid, uintptr(sp), b)
		if err != nil {
			panic(err)
		}

		// The address to return to is at the top of the frame
		content := binary.LittleEndian.Uint64(b[i : i+8])
		_, lineno, nextfn := d.SymTable.PCToLine(content)
		if nextfn != nil {
			d.Fn = nextfn
			fmt.Printf("  called by %s line %d\n", d.Fn.Name, lineno)
		}

		for i = 8; sp+i <= bp; i += 8 {
			content := binary.LittleEndian.Uint64(b[i : i+8])
			if sp+i == bp {
				nextbp = content
			}
		}

		if d.Fn.Name == "main.main" || d.Fn.Name == "runtime.main" {
			break
		}

		// Move to the next frame
		sp = sp + i
		bp = nextbp
	}

	fmt.Println()
}

// RunTarget starts the target executable and handles the debugging session.
func (d *Debugger) RunTarget(target string) {
	cmd := exec.Command(target)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Ptrace: true,
	}

	cmd.Start()
	err := cmd.Wait()
	if err != nil {
		fmt.Printf("Wait returned: %v\n\n", err)
	}

	pid := cmd.Process.Pid
	pgid, _ := syscall.Getpgid(pid)

	must(syscall.PtraceSetOptions(pid, syscall.PTRACE_O_TRACECLONE))

	if d.InputOrContinue(pid) {
		must(syscall.PtraceCont(pid, 0))
	} else {
		must(syscall.PtraceSingleStep(pid))
	}

	for {
		wpid, err := syscall.Wait4(-1*pgid, &d.Ws, 0, nil)
		must(err)
		if d.Ws.Exited() {
			if wpid == pid {
				break
			}
		} else {
			if d.Ws.StopSignal() == syscall.SIGTRAP && d.Ws.TrapCause() != syscall.PTRACE_EVENT_CLONE {
				must(syscall.PtraceGetRegs(wpid, &d.Regs))
				filename, line, fn := d.SymTable.PCToLine(d.Regs.Rip)
				fmt.Printf("Stopped at %s at %d in %s\n", fn.Name, line, filename)
				d.OutputStack(wpid, d.Regs.Rip, d.Regs.Rsp, d.Regs.Rbp)

				if d.BreakpointSet {
					d.ReplaceCode(wpid, d.PC, d.OriginalCode)
					d.BreakpointSet = false
				}

				if d.InputOrContinue(wpid) {
					must(syscall.PtraceCont(wpid, 0))
				} else {
					must(syscall.PtraceSingleStep(wpid))
				}
			} else {
				must(syscall.PtraceCont(wpid, 0))
			}
		}
	}
}

// Run starts the debugging session.
func (d *Debugger) Run() {
	target := os.Args[1]
	d.SymTable = d.GetSymbolTable(target)
	d.Fn = d.SymTable.LookupFunc("main.main")
	d.TargetFile, d.Line, d.Fn = d.SymTable.PCToLine(d.Fn.Entry)
	d.RunTarget(target)
}
