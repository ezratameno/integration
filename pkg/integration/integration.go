package integration

import (
	"context"
	"errors"
	"fmt"

	giteasdk "code.gitea.io/sdk/gitea"
	"github.com/ezratameno/integration/pkg/flux"
	"github.com/ezratameno/integration/pkg/gitea"
	"github.com/ezratameno/integration/pkg/kind"
	"k8s.io/apimachinery/pkg/types"
)

type Opts struct {

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

	// Flux

	// Path in the local repo that we should bootstrap from
	FluxPath string
}

type Client struct {
	opts        Opts
	kindClient  *kind.Client
	giteaClient *gitea.Client
	fluxClient  *flux.Client
}

func NewClient(opts Opts) (*Client, error) {

	// Set default

	if opts.GiteaHttpPort == 0 {
		opts.GiteaHttpPort = 3000
	}

	if opts.GiteaSshPort == 0 {
		opts.GiteaSshPort = 2222
	}

	if opts.GiteaUsername == "" {
		opts.GiteaUsername = "labuser"
	}

	if opts.GiteaPassword == "" {
		opts.GiteaPassword = "adminlabuser"
	}

	if opts.GiteaRepoName == "" {
		opts.GiteaRepoName = "test"
	}

	if opts.GiteaLocalRepoPath == "" {
		return nil, fmt.Errorf("local repo path is required")
	}

	// TODO: maybe generate a rand number so we can run multiple env?
	if opts.PrivateKeyPath == "" {
		opts.PrivateKeyPath = "/tmp/gitea-key.pem"
	}

	if opts.KindClusterName == "" {
		opts.KindClusterName = "integration"
	}

	if opts.KindConfigPath == "" {
		return nil, fmt.Errorf("kind config path is required")
	}

	giteaOpts := gitea.Opts{
		Addr:     "http://localhost",
		SSHPort:  opts.GiteaSshPort,
		HttpPort: opts.GiteaHttpPort,
	}

	giteaClient := gitea.NewClient(giteaOpts)

	kindClient := kind.NewClient()

	fluxClient, err := flux.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create flux client: %w", err)
	}

	c := &Client{
		opts:        opts,
		giteaClient: giteaClient,
		kindClient:  kindClient,
		fluxClient:  fluxClient,
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

func (c *Client) StartIntegration(ctx context.Context) (func() error, error) {

	var cancelFuncs cancelFuncs

	containerName, err := c.SetUpGitea(ctx)
	if err != nil {
		return func() error { return gitea.Close(containerName) },
			fmt.Errorf("failed to set up gitea: %w", err)
	}

	cancelFuncs = append(cancelFuncs, func() error {
		return gitea.Close(containerName)
	})

	// Create cluster
	err = c.kindClient.CreateClusterWithConfig(c.opts.KindClusterName, c.opts.KindConfigPath)
	if err != nil {
		return cancelFuncs.CancelFunc(), fmt.Errorf("failed to create kind cluster: %w", err)
	}

	cancelFuncs = append(cancelFuncs, func() error {
		return c.kindClient.DeleteCluster(c.opts.KindClusterName)
	})

	// Bootstrap

	ip, err := getOutboundIP()
	if err != nil {
		return cancelFuncs.CancelFunc(), err
	}

	bootstrapOpts := flux.BootstrapOpts{
		PrivateKeyPath: c.opts.PrivateKeyPath,
		Branch:         "main",
		Path:           c.opts.FluxPath,
		Password:       c.opts.GiteaPassword,
		Username:       c.opts.GiteaUsername,
		Url:            fmt.Sprintf("localhost:%d/%s/%s.git", c.opts.GiteaSshPort, c.opts.GiteaUsername, c.opts.GiteaRepoName),
		GitRepoUrl:     fmt.Sprintf("http://%s:%d/%s/%s.git", ip.String(), c.opts.GiteaHttpPort, c.opts.GiteaUsername, c.opts.GiteaRepoName),
	}

	err = c.fluxClient.Bootstrap(ctx, bootstrapOpts)
	if err != nil {
		return cancelFuncs.CancelFunc(), fmt.Errorf("failed to bootstrap: %w", err)
	}

	return cancelFuncs.CancelFunc(), nil

}

// TODO: do i need to delete the gitea container if the operation failed?
func (c *Client) SetUpGitea(ctx context.Context) (string, error) {

	signUpOpts := gitea.SignUpOpts{
		Email:    fmt.Sprintf("%s@gmail.com", c.opts.GiteaUsername),
		Password: c.opts.GiteaPassword,
		Username: c.opts.GiteaUsername,
	}

	// Start github
	containerName, err := c.giteaClient.Start(ctx, signUpOpts)
	if err != nil {
		return containerName, fmt.Errorf("failed to start gitea: %w", err)
	}

	_, err = c.giteaClient.GeneratePrivatePublicKeys(c.opts.GiteaRepoName, c.opts.PrivateKeyPath)
	if err != nil {
		return containerName, fmt.Errorf("failed to generate public and private keys: %w", err)
	}

	repoOpts := giteasdk.CreateRepoOption{
		Name:       c.opts.GiteaRepoName,
		TrustModel: giteasdk.TrustModelCollaboratorCommitter,
	}

	_, err = c.giteaClient.CreateRepoFromExisting(ctx, repoOpts, c.opts.GiteaLocalRepoPath)
	if err != nil {
		return containerName, fmt.Errorf("failed to create gitea repo with local repo files: %w", err)
	}

	return containerName, nil
}

func (c *Client) WaitForKs(ctx context.Context, kss ...types.NamespacedName) error {
	return c.fluxClient.WaitForKs(ctx, kss...)
}
