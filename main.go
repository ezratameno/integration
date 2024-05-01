package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ezratameno/integration/pkg/gitea"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {

	// TODO: we need to start the docker container
	opts := gitea.Opts{
		Addr:     "http://localhost",
		SSHPort:  2222,
		HttpPort: 3000,
	}
	client := gitea.NewClient(opts)

	err := client.Start(context.Background())
	if err != nil {
		return err
	}
	return nil
}
