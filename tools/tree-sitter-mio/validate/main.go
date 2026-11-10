// Command validate prints, for each Mio source file given, whether the official
// Mio parser accepts it ("OK") or rejects it ("REJECT"). It is used by
// check.sh to compare the Tree-sitter grammar with the official parser.
package main

import (
	"fmt"
	"os"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

func main() {
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("REJECT %s\n", path)
			continue
		}
		lex, err := lexer.NewChecked(string(data), token.V05)
		if err != nil {
			fmt.Printf("REJECT %s\n", path)
			continue
		}
		if _, err := parser.New(lex).ParseProgram(); err != nil {
			fmt.Printf("REJECT %s\n", path)
			continue
		}
		fmt.Printf("OK %s\n", path)
	}
}
