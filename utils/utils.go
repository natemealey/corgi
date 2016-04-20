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

type ColorStruct struct {
	disabled bool
}

var Color ColorStruct

var defaultColor = "39"

func (c *ColorStruct) Black(str string) string {
	if c.disabled {
		return str
	}
	return "\033[30m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) Red(str string) string {
	if c.disabled {
		return str
	}
	return "\033[31m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) Green(str string) string {
	if c.disabled {
		return str
	}
	return "\033[32m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) Yellow(str string) string {
	if c.disabled {
		return str
	}
	return "\033[33m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) Blue(str string) string {
	if c.disabled {
		return str
	}
	return "\033[34m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) Magenta(str string) string {
	if c.disabled {
		return str
	}
	return "\033[35m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) Cyan(str string) string {
	if c.disabled {
		return str
	}
	return "\033[36m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) White(str string) string {
	if c.disabled {
		return str
	}
	return "\033[37m" + str + "\033[" + defaultColor + "m"
}
func (c *ColorStruct) DarkGray(str string) string {
	if c.disabled {
		return str
	}
	return "\033[1;30m" + str + "\033[" + defaultColor + "m"
}
