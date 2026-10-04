package main

import (
	"embed"
	"os"

	"github.com/pablovarela/media-server-engine/cmd"
)

//go:embed docker-compose.yml docker-compose.monitoring.yml grafana prometheus
var engine embed.FS

func main() {
	os.Exit(cmd.Execute(engine))
}
