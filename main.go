package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts := integration.Opts{
		GiteaSshPort:       2222,
		GiteaHttpPort:      3000,
		GiteaRepoName:      "ezra",
		GiteaLocalRepoPath: "/home/etameno/etameno/Desktop/github/habana-k8s-infra-services",
		FluxPath:           "flux/clusters/dc02",
		KindConfigPath:     "/home/etameno/etameno/Desktop/github/habana-k8s-infra-services/test/kind-cluster/kind-cluster.yaml",
		KindClusterName:    "test2",
	}

	client, err := integration.NewClient(opts)
	if err != nil {
		return err
	}

	ch := client.StartIntegration(ctx)

	for {
		select {
		case i := <-ch:
			if i.Err != nil {
				return i.Err
			}

			fmt.Println(i.Msg)
		case <-ctx.Done():

			return ctx.Err()

		}

	}

	// return nil
}
