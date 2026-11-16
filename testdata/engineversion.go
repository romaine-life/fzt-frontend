// Prints the fzt engine version a binary built against this module reports.
package main

import (
	"fmt"

	frontend "github.com/romaine-life/fzt-frontend"
)

func main() { fmt.Print(frontend.EngineVersion) }
