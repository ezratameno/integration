package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ezratameno/integration/pkg/integration"
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

	opts := integration.Opts{
		GiteaSshPort:       2222,
		GiteaHttpPort:      3000,
		GiteaRepoName:      "ezra",
		GiteaLocalRepoPath: "/home/ezra/Desktop/k8s-flux",
		FluxPath:           "flux",
	}

	client, err := integration.NewClient(opts)
	if err != nil {
		return err
	}

	err = client.StartIntegration(context.Background())
	if err != nil {
		return err
	}
	return nil
}
