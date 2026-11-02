// Command lmw is a command-line client for LinkedIn.
package main

import (
	"os"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
