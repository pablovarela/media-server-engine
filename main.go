package main

import (
	"os"

	"github.com/pablovarela/media-server-engine/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
