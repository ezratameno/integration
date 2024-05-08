package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	giteasdk "code.gitea.io/sdk/gitea"
	"github.com/ezratameno/integration/pkg/exec"
	"github.com/ezratameno/integration/pkg/flux"
	"github.com/ezratameno/integration/pkg/gitea"
	"github.com/ezratameno/integration/pkg/kind"
	"k8s.io/apimachinery/pkg/types"
)

type Client struct {
	kindClient  *kind.Client
	giteaClient *gitea.Client
	fluxClient  *flux.Client
	out         io.Writer
}

func NewClient(opts gitea.Opts, out io.Writer) (*Client, error) {

	giteaClient := gitea.NewClient(opts, out)

	kindClient := kind.NewClient()

	fluxClient, err := flux.NewClient(out)
	if err != nil {
		return nil, fmt.Errorf("failed to create flux client: %w", err)
	}

	c := &Client{
		giteaClient: giteaClient,
		kindClient:  kindClient,
		fluxClient:  fluxClient,
		out:         out,
	}

	return c, nil
}

type cancelFuncs []func() error

func (c cancelFuncs) CancelFunc() func() error {

	return func() error {
		var genErr error
		for _, cancelFunc := range c {
			err := cancelFunc()
			genErr = errors.Join(genErr, err)
		}

		return genErr
	}

}

type CreateOpts struct {

	// Gitea

	GiteaSshPort  int
	GiteaHttpPort int

	// Gitea username to create as admin
	GiteaUsername string

	// Gitea password of the user to create as admin
	GiteaPassword string

	// Repo name to create
	GiteaRepoName string

	// Path to a local repo which be uploaded to the new gitea repo
	GiteaLocalRepoPath string

	// PrivateKeyPath where to save private key
	PrivateKeyPath string

	// Kind

	KindClusterName string

	// Path to a kind config
	KindConfigPath string

	// Image to load to the kind cluster
	KindImageToLoad []string

	// Flux

	// Path in the local repo that we should bootstrap from
	FluxPath string

	GiteaContainerName string

	// Path to kubernetes manifests to apply
	ManifestsToApply []string

	// Wait for those kustomizations to be ready
	KustomizationsToWaitFor []types.NamespacedName
}

func (c *Client) Run(ctx context.Context, opts CreateOpts) (func() error, error) {

	err := validateCreateOpts(&opts)
	if err != nil {
		return func() error { return nil }, err
	}
	cancelFunc, err := c.StartEnv(ctx, opts)
	if err != nil {
		return cancelFunc, err
	}

	if len(opts.ManifestsToApply) > 0 {
		fmt.Fprintln(c.out, "applying manifests")
	}

	err = applyManifest(ctx, opts.ManifestsToApply...)
	if err != nil {
		return cancelFunc, fmt.Errorf("failed to apply manifests: %w", err)
	}

	if len(opts.ManifestsToApply) > 0 {
		fmt.Fprintln(c.out, "applied manifests")
	}

	if len(opts.KustomizationsToWaitFor) > 0 {
		fmt.Fprintln(c.out, "waiting for kustomizations to be ready")
	}

	err = c.fluxClient.WaitForKs(ctx, opts.KustomizationsToWaitFor...)
	if err != nil {
		return cancelFunc, fmt.Errorf("failed to wait for kustomizations: %w", err)
	}

	if len(opts.KustomizationsToWaitFor) > 0 {
		fmt.Fprintln(c.out, "finish waiting for kustomizations")
	}

	return cancelFunc, nil
}

type DeleteOpts struct {
	KindClusterName    string
	GiteaContainerName string
}

// Delete will delete the kind cluster and the gitea container.
func (c *Client) Delete(ctx context.Context, opts DeleteOpts) error {

	var genErr error

	err := c.kindClient.DeleteCluster(opts.KindClusterName)
	if err != nil {
		genErr = errors.Join(genErr, err)
	}

	err = c.giteaClient.Delete(ctx, opts.GiteaContainerName)
	if err != nil {
		genErr = errors.Join(genErr, err)
	}

	return genErr
}

func applyManifest(ctx context.Context, manifests ...string) error {

	for _, manifest := range manifests {
		cmd := fmt.Sprintf("kubectl apply -f %s", manifest)
		buf := bytes.Buffer{}
		err := exec.LocalExecContext(ctx, cmd, &buf)
		if err != nil {
			return err
		}
	}

	return nil
}
func (c *Client) StartEnv(ctx context.Context, opts CreateOpts) (func() error, error) {

	var cancelFuncs cancelFuncs

	// TODO: Maybe start gitea and kind cluster both at the same time? save a little time
	containerName, err := c.SetUpGitea(ctx, opts)
	if err != nil {
		return func() error { return c.giteaClient.Delete(context.TODO(), containerName) },
			fmt.Errorf("failed to set up gitea: %w", err)
	}

	cancelFuncs = append(cancelFuncs, func() error {
		return c.giteaClient.Delete(context.TODO(), containerName)
	})

	fmt.Fprintln(c.out, "finish setting gitea")

	fmt.Fprintln(c.out, "creating kind cluster")

	// Create cluster
	err = c.kindClient.CreateClusterWithConfig(opts.KindClusterName, opts.KindConfigPath)
	if err != nil {
		return cancelFuncs.CancelFunc(), fmt.Errorf("failed to create kind cluster: %w", err)
	}

	fmt.Fprintln(c.out, "kind cluster created")

	cancelFuncs = append(cancelFuncs, func() error {
		return c.kindClient.DeleteCluster(opts.KindClusterName)
	})

	if len(opts.KindImageToLoad) > 0 {
		fmt.Fprintln(c.out, "loading images to kind cluster")
	}

	// load images
	for _, image := range opts.KindImageToLoad {
		var buf bytes.Buffer
		cmd := fmt.Sprintf("kind load docker-image %s --name %s", image, opts.KindClusterName)
		err := exec.LocalExecContext(ctx, cmd, &buf)
		if err != nil {

			// Ignore error when image not present on local host
			if strings.Contains(buf.String(), "not present locally") {
				fmt.Fprintf(c.out, "image %s is not present locally, will not load \n", image)
				continue
			}
			return cancelFuncs.CancelFunc(), fmt.Errorf("failed to load image %s: %s %w", image, buf.String(), err)
		}
	}

	if len(opts.KindImageToLoad) > 0 {
		fmt.Fprintln(c.out, "loaded images")
	}

	// Bootstrap

	ip, err := getOutboundIP()
	if err != nil {
		return cancelFuncs.CancelFunc(), err
	}

	bootstrapOpts := flux.BootstrapOpts{
		PrivateKeyPath: opts.PrivateKeyPath,
		Branch:         "main",
		Path:           opts.FluxPath,
		Password:       opts.GiteaPassword,
		Username:       opts.GiteaUsername,
		Url:            fmt.Sprintf("localhost:%d/%s/%s.git", opts.GiteaSshPort, opts.GiteaUsername, opts.GiteaRepoName),
		GitRepoUrl:     fmt.Sprintf("http://%s:%d/%s/%s.git", ip.String(), opts.GiteaHttpPort, opts.GiteaUsername, opts.GiteaRepoName),
	}

	err = c.fluxClient.Bootstrap(ctx, bootstrapOpts)
	if err != nil {
		return cancelFuncs.CancelFunc(), fmt.Errorf("failed to bootstrap: %w", err)
	}

	return cancelFuncs.CancelFunc(), nil

}

// TODO: do i need to delete the gitea container if the operation failed?
func (c *Client) SetUpGitea(ctx context.Context, opts CreateOpts) (string, error) {

	signUpOpts := gitea.StartContainerOpts{
		Email:         fmt.Sprintf("%s@gmail.com", opts.GiteaUsername),
		Password:      opts.GiteaPassword,
		Username:      opts.GiteaUsername,
		ContainerName: opts.GiteaContainerName,
	}

	// Start github
	containerName, err := c.giteaClient.Start(ctx, signUpOpts)
	if err != nil {
		return containerName, fmt.Errorf("failed to start gitea: %w", err)
	}

	_, err = c.giteaClient.GeneratePrivatePublicKeys(opts.GiteaRepoName, opts.PrivateKeyPath)
	if err != nil {
		return containerName, fmt.Errorf("failed to generate public and private keys: %w", err)
	}

	repoOpts := giteasdk.CreateRepoOption{
		Name:       opts.GiteaRepoName,
		TrustModel: giteasdk.TrustModelCollaboratorCommitter,
	}

	_, err = c.giteaClient.CreateRepoFromExisting(ctx, repoOpts, opts.GiteaLocalRepoPath)
	if err != nil {
		return containerName, fmt.Errorf("failed to create gitea repo with local repo files: %w", err)
	}

	return containerName, nil
}

func (c *Client) WaitForKs(ctx context.Context, kss ...types.NamespacedName) error {
	return c.fluxClient.WaitForKs(ctx, kss...)
}
