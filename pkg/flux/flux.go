package flux

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ezratameno/integration/pkg/exec"
	helmv2 "github.com/fluxcd/helm-controller/api/v2beta2"
	apimeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type Client struct {
	kubeClient client.Client
}

func NewClient() (*Client, error) {

	c := &Client{}

	return c, nil
}

type BootstrapOpts struct {
	PrivateKeyPath string
	Branch         string
	Path           string
	Password       string
	Username       string
	Url            string

	// url to update the gitrepo object
	GitRepoUrl string
}

func (c *Client) Initialize() error {
	// register the GitOps Toolkit schema definitions
	scheme := runtime.NewScheme()
	_ = sourcev1.AddToScheme(scheme)
	_ = helmv2.AddToScheme(scheme)

	// init Kubernetes client
	kubeClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	c.kubeClient = kubeClient
	return nil
}

func (c *Client) Bootstrap(ctx context.Context, opts BootstrapOpts) error {

	err := c.Initialize()
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf(`flux bootstrap git --url="ssh://git@%s" --branch="%s" --private-key-file="%s" --path="%s" --password="%s" --username="%s" --token-auth=true`,
		opts.Url, opts.Branch, opts.PrivateKeyPath, opts.Path, opts.Password, opts.Username)

	fmt.Println(cmd)

	// Bootstrap flux
	var buf bytes.Buffer

	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// wait until the git repo is created
	go func() {

		defer cancel()
		for {
			time.Sleep(1 * time.Second)

			s := buf.String()

			if len(s) == 0 {
				continue
			}

			fmt.Println(s)

			if strings.Contains(s, `reconciled sync configuration`) {
				return
			}

		}
	}()
	// TODO: handle better
	err = exec.LocalExecContext(cmdCtx, cmd, &buf)
	if err != nil && !strings.Contains(err.Error(), "signal: killed") {
		return err
	}

	// TODO: read from buffer until i get some string that indicate that resource was created
	// waiting for GitRepository "flux-system/flux-system" to be reconciled

	// TODO: maybe i need to use informers? or one time is enough?
	// Update the url
	gitRepo := &sourcev1.GitRepository{}
	c.kubeClient.Get(ctx, types.NamespacedName{
		Namespace: "flux-system",
		Name:      "flux-system",
	}, gitRepo)

	// TODO: improve this, maybe it's too specific?
	gitRepo.Spec.URL = opts.GitRepoUrl

	fmt.Println("url", gitRepo.Spec.URL)

	err = c.kubeClient.Patch(ctx, gitRepo, client.Merge, &client.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch changes: %w", err)
	}

	// TODO: wait until git repo is in status ready

	// wait for the a Helm release to be reconciled
	fmt.Println("Waiting for git repo to be ready")
	err = wait.PollUntilContextCancel(ctx, 2*time.Second, true,
		func(ctx context.Context) (done bool, err error) {

			namespacedName := types.NamespacedName{
				Namespace: gitRepo.GetNamespace(),
				Name:      gitRepo.GetName(),
			}
			if err := c.kubeClient.Get(ctx, namespacedName, gitRepo); err != nil {
				return false, err
			}
			return meta.IsStatusConditionTrue(gitRepo.Status.Conditions, apimeta.ReadyCondition), nil
		})

	if err != nil {
		return err
	}

	// TODO: make sure all kustomizations are running ok? (reconcile?)

	return nil
}
