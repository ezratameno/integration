package integration

import (
	"context"
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

type Info struct {
	Err error
	Msg Message
}

type Message int

func (m Message) String() string {
	switch m {
	case MessageGiteaReady:
		return "gitea is ready"
	case MessageKindReady:
		return "kind is ready"
	case MessageFluxReady:
		return "flux is ready"
	default:
		return fmt.Sprintf("%d", int(m))
	}
}

const (
	MessageUnknown Message = iota
	MessageGiteaReady
	MessageKindReady
	MessageFluxReady
)

// TODO: maybe should return a chan that update on progress?
// TODO: clean up once the context is done
// TODO: do i want this to run in a goroutine?
func (c *Client) StartIntegration(ctx context.Context) chan Info {

	// ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	// defer cancel()

	ch := make(chan Info)

	go func() {
		containerName, err := c.SetUpGitea(ctx)
		if err != nil {
			defer gitea.Close(containerName)
			ch <- Info{
				Err: fmt.Errorf("failed to set up gitea: %w", err),
			}
			return
		}
		defer gitea.Close(containerName)

		ch <- Info{
			Msg: MessageGiteaReady,
		}

		// Create cluster
		err = c.kindClient.CreateClusterWithConfig(c.opts.KindClusterName, c.opts.KindConfigPath)
		if err != nil {
			ch <- Info{
				Err: fmt.Errorf("failed to create kind cluster: %w", err),
			}
			return
		}
		defer c.kindClient.DeleteCluster(c.opts.KindClusterName)

		ch <- Info{
			Msg: MessageKindReady,
		}

		// Bootstrap

		ip, err := getOutboundIP()
		if err != nil {

			ch <- Info{
				Err: err,
			}
			return
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
			ch <- Info{
				Err: fmt.Errorf("failed to bootstrap: %w", err),
			}
			return
		}

		ch <- Info{
			Msg: MessageFluxReady,
		}

		<-ctx.Done()
	}()

	return ch

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
