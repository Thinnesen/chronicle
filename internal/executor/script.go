package executor

import (
	"os"
	"strconv"
	"unicode/utf8"
)

// scriptPart is the largest part of a script source in one environment variable. Linux refuses a variable larger
// than 128 KiB with "argument list too long".
const scriptPart = 64 << 10

// ScriptEnvName is the name of the environment variable with part i of the script source.
func ScriptEnvName(i int) string {
	if i == 0 {
		return EnvScript
	}
	return EnvScript + "_" + strconv.Itoa(i)
}

// SplitScript cuts a script source into parts for environment variables. It cuts between characters, because
// Kubernetes needs valid UTF-8.
func SplitScript(src string) []string {
	var parts []string
	for len(src) > scriptPart {
		cut := scriptPart
		for cut > 0 && !utf8.RuneStart(src[cut]) {
			cut--
		}
		parts = append(parts, src[:cut])
		src = src[cut:]
	}
	return append(parts, src)
}

// readScript joins the parts of the script source, or returns false without a script.
func readScript() (string, bool) {
	src, ok := os.LookupEnv(EnvScript)
	for i := 1; ok; i++ {
		part, more := os.LookupEnv(ScriptEnvName(i))
		if !more {
			break
		}
		src += part
	}
	return src, ok
}
