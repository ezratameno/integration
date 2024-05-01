package main

import (
	"context"
	"fmt"
	"os"
	"time"

	giteasdk "code.gitea.io/sdk/gitea"

	"github.com/ezratameno/integration/pkg/gitea"
	"github.com/ezratameno/integration/pkg/kind"
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
	giteaOpts := gitea.Opts{
		Addr:     "http://localhost",
		SSHPort:  2222,
		HttpPort: 3000,
	}
	client := gitea.NewClient(giteaOpts)

	err := client.Start(context.Background())
	if err != nil {
		return err
	}

	pubKey, err := client.GeneratePrivatePublicKeys("test", "/tmp/gitea-key.pem")
	if err != nil {
		return err
	}

	fmt.Println(pubKey.ID)

	repoOpts := giteasdk.CreateRepoOption{
		Name:       "test",
		TrustModel: giteasdk.TrustModelCollaboratorCommitter,
	}
	_, err = client.CreateRepoFromExisting(repoOpts, "/home/ezra/Desktop/k8s-flux")
	if err != nil {
		return err
	}

	fmt.Println("finish gitea flow")
	// Start kind cluster

	kindClient := kind.NewClient()

	clusterName := "integration"
	err = kindClient.CreateClusterWithConfig(clusterName, "kind/kind-multinode.yaml")
	if err != nil {
		return err
	}

	fmt.Println("created cluster!")

	defer kindClient.DeleteCluster(clusterName)

	time.Sleep(1 * time.Minute)
	return nil
}
