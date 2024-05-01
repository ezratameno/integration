package main

import (
	"context"
	"fmt"
	"os"
	"time"

	giteasdk "code.gitea.io/sdk/gitea"

	"github.com/ezratameno/integration/pkg/flux"
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

	signUpOpts := gitea.SignUpOpts{
		Email:    "admin@gmail.com",
		Password: "adminlabuser",
		Username: "labuser",
	}
	err := client.Start(context.Background(), signUpOpts)
	if err != nil {
		return err
	}

	privateKeyPath := "/tmp/gitea-key.pem"
	pubKey, err := client.GeneratePrivatePublicKeys("test", privateKeyPath)
	if err != nil {
		return err
	}

	fmt.Println(pubKey.ID)

	repoName := "test"
	repoOpts := giteasdk.CreateRepoOption{
		Name:       repoName,
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

	// Flux

	fluxOpts := flux.Opts{
		PrivateKeyPath: privateKeyPath,
		Branch:         "main",
		Path:           "flux",
		Password:       signUpOpts.Password,
		Username:       signUpOpts.Username,
		Url:            fmt.Sprintf("localhost:%d/%s/%s.git", giteaOpts.SSHPort, signUpOpts.Username, repoName),
	}

	fluxClient, err := flux.NewClient(fluxOpts)
	if err != nil {
		return err
	}

	err = fluxClient.Bootstrap(context.Background())
	if err != nil {
		return fmt.Errorf("failed to bootstrap: %w", err)
	}
	time.Sleep(10 * time.Minute)
	return nil
}
