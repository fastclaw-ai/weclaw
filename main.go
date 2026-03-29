package main

import (
	"io/fs"
	"log"

	"github.com/fastclaw-ai/weclaw/cmd"
)

func main() {
	sub, err := fs.Sub(webUIFS, "web/out")
	if err != nil {
		log.Printf("[webui] embedded web UI not available: %v", err)
	} else {
		cmd.WebUIFS = sub
	}
	cmd.Execute()
}
