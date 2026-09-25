package main

import (
	"fmt"

	"claude-webext-patcher/gui"
	"claude-webext-patcher/status"
)

// Checklist rows, in display order. rowSandbox (Linux AppArmor) and rowCowork
// (Windows) only appear when they're needed.
const (
	rowLauncher   = "launcher"
	rowClaude     = "claude"
	rowSandbox    = "sandbox"
	rowPatch      = status.StepPatch
	rowExtensions = status.StepExtensions
	rowCowork     = status.StepCowork
	rowLaunch     = "launch"
)

func checklistRows(withSandbox, withCowork bool) []gui.Row {
	rows := []gui.Row{
		{ID: rowLauncher, Label: "Launcher update"},
		{ID: rowClaude, Label: "Claude", Download: true},
	}
	if withSandbox {
		rows = append(rows, gui.Row{ID: rowSandbox, Label: "Sandbox permission"})
	}
	rows = append(rows,
		gui.Row{ID: rowPatch, Label: "Patching"},
		gui.Row{ID: rowExtensions, Label: "Extensions"},
	)
	if withCowork {
		rows = append(rows, gui.Row{ID: rowCowork, Label: "Cowork service"})
	}
	return append(rows, gui.Row{ID: rowLaunch, Label: "Launching Claude"})
}

// userInterface is how the launcher's flow talks to the user: the window
// (*gui.Status) normally, the terminal with --debug.
type userInterface interface {
	// SetRow updates a checklist row (states from the status package). An empty label
	// keeps the current one.
	SetRow(id, state, label, note string)
	// DownloadProgress reports bytes downloaded so far.
	DownloadProgress(done, total int64)
	// Ask poses a multiple-choice question and returns the chosen index, or -1.
	Ask(question, detail string, options []string) int
}

var ui userInterface = terminalUI{}

// terminalUI is the --debug interface: rows become log lines, questions a prompt.
type terminalUI struct{}

func (terminalUI) SetRow(id, state, label, note string) {
	if label == "" {
		label = id
	}
	if note != "" {
		label += " (" + note + ")"
	}
	fmt.Printf("[%s] %s\n", state, label)
}

func (terminalUI) DownloadProgress(done, total int64) {}

func (terminalUI) Ask(question, detail string, options []string) int {
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println(question)
	fmt.Println()
	fmt.Println(detail)
	fmt.Println()
	for i, o := range options {
		fmt.Printf("[%d] %s\n", i+1, o)
	}
	fmt.Println("============================================================")
	fmt.Print("Choose: ")

	var input string
	fmt.Scanln(&input)
	for i := range options {
		if input == fmt.Sprint(i+1) {
			return i
		}
	}
	return -1
}
