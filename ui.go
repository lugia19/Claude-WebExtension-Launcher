package main

import "fmt"

// userInterface is how the launcher's flow talks to the user: the status window
// (gui.Status) normally, the terminal with --debug.
type userInterface interface {
	// Step reports the current phase ("Checking for Claude updates...").
	Step(text string)
	// Ask poses a multiple-choice question and returns the chosen index, or -1.
	Ask(question, detail string, options []string) int
}

var ui userInterface = terminalUI{}

// terminalUI is the --debug / no-window interface. Steps are already visible as log
// output, so only questions print anything.
type terminalUI struct{}

func (terminalUI) Step(string) {}

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
