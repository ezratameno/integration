package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ezratameno/integration/pkg/gitea"
	"github.com/ezratameno/integration/pkg/integration"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
)

func main() {
	ctx := context.Background()
	err := run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)

	}
}

func run(ctx context.Context) error {
	switch os.Args[1] {
	case "create":
		return createCmd(ctx, os.Args[2:])
	case "delete":
		return deleteCmd(ctx, os.Args[2:])

		// TODO:
	// case "version":

	default:
		return fmt.Errorf("bad command")
	}
}

func deleteCmd(ctx context.Context, args []string) error {

	var deleteOpts integration.DeleteOpts

	f := flag.NewFlagSet("c", flag.ContinueOnError)
	f.StringVar(&deleteOpts.KindClusterName, "cluster", "integration", "the name of the kind cluster to be created")
	f.StringVar(&deleteOpts.GiteaContainerName, "container", "gitea", "the name of the gitea container")
	err := f.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	client, err := integration.NewClient(gitea.Opts{}, os.Stdout)
	if err != nil {
		return err
	}

	return client.Delete(ctx, deleteOpts)
}

func createCmd(ctx context.Context, args []string) error {
	var createOpts integration.CreateOpts

	f := flag.NewFlagSet("c", flag.ContinueOnError)
	f.IntVar(&createOpts.GiteaHttpPort, "http-port", 3000, "gitea http port")
	f.IntVar(&createOpts.GiteaSshPort, "ssh-port", 2222, "gitea ssh port")
	f.StringVar(&createOpts.GiteaLocalRepoPath, "local-repo", "", "path to a local git repo")
	f.StringVar(&createOpts.FluxPath, "flux-path", "", "path to bootstrap flux with within the local git repo")
	f.StringVar(&createOpts.KindConfigPath, "kind-config", "", "path to kind cluster config")
	f.StringVar(&createOpts.KindClusterName, "cluster", "integration", "the name of the kind cluster to be created")
	f.StringVar(&createOpts.GiteaContainerName, "container", "gitea", "the name of the gitea container")

	images := f.String("kind-images", "", "comma separated list of images to load to the kind cluster")
	manifests := f.String("manifests", "", "comma separated list of kubernetes manifests to apply")
	kustomizations := f.String("kustomizations", "", "comma separated list of kustomizations in the format namespace/name")

	err := f.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	// For empty string
	if !(len(strings.Split(*images, ",")) == 1 && strings.Split(*images, ",")[0] == "") {
		createOpts.KindImageToLoad = strings.Split(*images, ",")
	}

	if !(len(strings.Split(*manifests, ",")) == 1 && strings.Split(*manifests, ",")[0] == "") {
		createOpts.ManifestsToApply = strings.Split(*manifests, ",")
	}

	for _, ks := range strings.Split(*kustomizations, ",") {

		if ks == "" {
			continue
		}
		data := strings.Split(ks, "/")
		if len(data) != 2 {
			return fmt.Errorf("invalid kustomization format: %s", ks)
		}

		createOpts.KustomizationsToWaitFor = append(createOpts.KustomizationsToWaitFor, types.NamespacedName{
			Namespace: data[0],
			Name:      data[1],
		})
	}

	fmt.Printf("%+v\n", createOpts)

	giteaOpts := gitea.Opts{
		Addr:     "http://localhost",
		SSHPort:  createOpts.GiteaSshPort,
		HttpPort: createOpts.GiteaHttpPort,
	}

	log := logrus.New().WithField("service", "cli")

	w := writer{
		log: log,
	}

	defer w.Write([]byte("finish"))

	client, err := integration.NewClient(giteaOpts, w)
	if err != nil {
		return err
	}

	_, err = client.Run(ctx, createOpts)
	if err != nil {
		return err
	}
	return nil
}

type writer struct {
	log *logrus.Entry
}

func (w writer) Write(p []byte) (n int, err error) {
	w.log.Info(string(p))
	return 0, nil
}
