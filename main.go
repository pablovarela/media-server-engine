package main

import (
	"embed"
	"os"

	"github.com/pablovarela/media-server-engine/cmd"
)

//go:embed docker-compose.yml docker-compose.monitoring.yml grafana prometheus homepage scripts/backup-excludes.txt all:config-template
var engine embed.FS

func main() {
	os.Exit(cmd.Execute(engine))
}
