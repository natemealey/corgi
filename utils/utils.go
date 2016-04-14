package utils

import (
	"bufio"
	"fmt"
	"strings"
)

// With the given prompt, get input, returning only when it's more than
// white space
func ReadWithPrompt(prompt string, r *bufio.Reader) string {
	val := ""
	for len(val) == 0 {
		fmt.Print(prompt)
		val, _ = r.ReadString('\n')
		val = strings.TrimSpace(val)
	}
	return val
}
