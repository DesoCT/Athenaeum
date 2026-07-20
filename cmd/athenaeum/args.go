package main

import "fmt"

// Go's flag package stops parsing at the first non-flag argument, so
// `athenaeum serve workspace/athenaeum.toml --safe-mode` would silently ignore
// --safe-mode. Silently dropping a security flag is unacceptable, so arguments
// are permuted before parsing: positional operands are separated from flags no
// matter what order the user typed them in.

// boolFlags never consume a following argument.
var boolFlags = map[string]bool{
	"no-open":   true,
	"remote":    true,
	"safe-mode": true,
	"pick":      true,
}

// valueFlags consume the following argument unless written as --flag=value.
var valueFlags = map[string]bool{
	"bind":            true,
	"port":            true,
	"log-level":       true,
	"auth-token-file": true,
	"registry":        true,
}

// splitArgs separates positional operands from flag arguments.
//
// A bare "--" terminates flag parsing; everything after it is positional.
func splitArgs(args []string) (positional, flags []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			return positional, flags, nil
		}

		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}

		name := trimFlagDashes(arg)
		// --flag=value carries its own value.
		if eq := indexByte(name, '='); eq >= 0 {
			flags = append(flags, arg)
			continue
		}

		switch {
		case boolFlags[name]:
			flags = append(flags, arg)
		case valueFlags[name]:
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("flag --%s needs a value", name)
			}
			flags = append(flags, arg, args[i+1])
			i++
		default:
			// Unknown flag: hand it to the flag package so it produces the
			// standard diagnostic and usage output.
			flags = append(flags, arg)
		}
	}
	return positional, flags, nil
}

func trimFlagDashes(arg string) string {
	name := arg
	for len(name) > 0 && name[0] == '-' {
		name = name[1:]
	}
	return name
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
