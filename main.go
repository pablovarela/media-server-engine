package main

import (
	"embed"
	"os"

	"github.com/pablovarela/media-server-engine/cmd"
)

//go:embed docker-compose.yml homepage scripts/backup-excludes.txt all:config-template
var engine embed.FS

func main() {
	os.Exit(cmd.Execute(engine))
}
