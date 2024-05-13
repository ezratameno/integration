package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	giteasdk "code.gitea.io/sdk/gitea"
	"github.com/ezratameno/integration/pkg/exec"
	"github.com/ezratameno/integration/pkg/flux"
	"github.com/ezratameno/integration/pkg/gitea"
	"github.com/ezratameno/integration/pkg/kind"
	"github.com/fluxcd/kustomize-controller/api/v1beta2"
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

	// Path to a local repo which be uploaded to the new gitea repo
	GiteaLocalRepoPaths []string

	// The name of the repo we should bootstrap flux with, should be on of the path in GiteaLocalRepoPaths
	FluxBootstrapRepo string

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

	// TODO: build dependency tree of ks and reconcile by the order

	deps, err := c.KsDeps(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to order kustomizations by deps: %w", err)
	}

	// reconcile by dep
	for _, dep := range deps {
		err = c.fluxClient.ReconcileKS(ctx, dep)
		if err != nil {
			fmt.Println(err)
			// return cancelFunc,
		}

	}

	if len(opts.KustomizationsToWaitFor) > 0 {
		fmt.Fprintln(c.out, "waiting for kustomizations to be ready")
	}

	err = c.WaitForKs(ctx, opts.KustomizationsToWaitFor...)
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

	type res struct {
		err        error
		cancelFunc func() error
	}

	resCh := make(chan res)
	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func(client *Client) {
		defer wg.Done()
		containerName, err := c.SetUpGitea(ctx, opts)
		cancelFunc := func() error {
			return nil
		}
		if err != nil {
			cancelFunc = func() error { return c.giteaClient.Delete(context.TODO(), containerName) }
		}

		resCh <- res{
			cancelFunc: cancelFunc,
			err:        err,
		}

	}(c)

	wg.Add(1)
	go func(client *Client) {
		defer wg.Done()
		cancelFunc, err := client.SetUpKind(ctx, opts)
		resCh <- res{
			cancelFunc: cancelFunc,
			err:        err,
		}
	}(c)

	// wait until
	go func() {
		wg.Wait()
		close(resCh)
		done <- struct{}{}
	}()

	// error from both gitea and kind
	var genErr error
	for i := range resCh {
		cancelFuncs = append(cancelFuncs, i.cancelFunc)
		if i.err != nil {
			genErr = errors.Join(genErr, i.err)
		}
	}

	// Wait until kind and gitea are ready
	<-done

	if genErr != nil {
		return cancelFuncs.CancelFunc(), genErr
	}

	// Bootstrap

	ip, err := getOutboundIP()
	if err != nil {
		return cancelFuncs.CancelFunc(), err
	}

	repoName := path.Base(opts.FluxBootstrapRepo)
	bootstrapOpts := flux.BootstrapOpts{
		PrivateKeyPath: opts.PrivateKeyPath,
		Branch:         "main",
		Path:           opts.FluxPath,
		Password:       opts.GiteaPassword,
		Username:       opts.GiteaUsername,
		HttpPort:       opts.GiteaHttpPort,
		Url:            fmt.Sprintf("localhost:%d/%s/%s.git", opts.GiteaSshPort, opts.GiteaUsername, repoName),
		GitRepoUrl:     fmt.Sprintf("http://%s:%d/%s/%s.git", ip.String(), opts.GiteaHttpPort, opts.GiteaUsername, repoName),
	}

	err = c.fluxClient.Bootstrap(ctx, bootstrapOpts)
	if err != nil {
		return cancelFuncs.CancelFunc(), fmt.Errorf("failed to bootstrap: %w", err)
	}

	return cancelFuncs.CancelFunc(), nil

}

func (c *Client) SetUpKind(ctx context.Context, opts CreateOpts) (func() error, error) {
	fmt.Fprintln(c.out, "creating kind cluster")

	// Create cluster
	err := c.kindClient.CreateClusterWithConfig(opts.KindClusterName, opts.KindConfigPath)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("failed to create kind cluster: %w", err)
	}

	fmt.Fprintln(c.out, "kind cluster created")

	cancelFunc := func() error {
		return c.kindClient.DeleteCluster(opts.KindClusterName)
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
			return cancelFunc, fmt.Errorf("failed to load image %s: %s %w", image, buf.String(), err)
		}
	}

	if len(opts.KindImageToLoad) > 0 {
		fmt.Fprintln(c.out, "loaded images")
	}

	fmt.Fprintln(c.out, "finish setting up kind cluster")

	return cancelFunc, nil
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

	_, err = c.giteaClient.GeneratePrivatePublicKeys("test", opts.PrivateKeyPath)
	if err != nil {
		return containerName, fmt.Errorf("failed to generate public and private keys: %w", err)
	}

	errCh := make(chan error)
	defer close(errCh)
	for _, repoPath := range opts.GiteaLocalRepoPaths {
		go func(clinet *Client, repoPath string) {

			repoName := path.Base(repoPath)
			repoOpts := giteasdk.CreateRepoOption{
				Name:       repoName,
				TrustModel: giteasdk.TrustModelCollaboratorCommitter,
			}

			_, err = c.giteaClient.CreateRepoFromExisting(ctx, repoOpts, repoPath)
			errCh <- err
		}(c, repoPath)

	}

	for i := 0; i < len(opts.GiteaLocalRepoPaths); i++ {
		err := <-errCh
		if err != nil {
			return containerName, fmt.Errorf("failed to create gitea repo with local repo files: %w", err)
		}
	}

	fmt.Fprintln(c.out, "finish setting up gitea")

	return containerName, nil
}

// KsDeps return the kustomizations by order of deps
func (c *Client) KsDeps(ctx context.Context) ([]types.NamespacedName, error) {

	kss, err := c.fluxClient.ListKs(ctx)
	if err != nil {
		return nil, err
	}

	deps := organizeKsByDeps(kss)

	return deps, nil
}

func organizeKsByDeps(kss []v1beta2.Kustomization) []types.NamespacedName {
	deps := make(map[types.NamespacedName][]types.NamespacedName)

	// collect on which ks the our ks is depends on
	for _, ks := range kss {
		ksInfo := types.NamespacedName{
			Namespace: ks.Namespace,
			Name:      ks.Name,
		}
		for _, d := range ks.Spec.DependsOn {

			deps[ksInfo] = append(deps[ksInfo], types.NamespacedName{
				Namespace: ks.Namespace,
				Name:      d.Name,
			})
		}
	}

	var res []types.NamespacedName

	// for each dependency of the kustomization get their dependencies
	for k := range deps {
		Dependencies(deps, &res, k)
		res = append(res, k)
	}

	res = removeDuplicate(res)

	fmt.Println(res)
	return res
}

func removeDuplicate[T comparable](sliceList []T) []T {
	allKeys := make(map[T]bool)
	list := []T{}
	for _, item := range sliceList {
		if _, value := allKeys[item]; !value {
			allKeys[item] = true
			list = append(list, item)
		}
	}
	return list
}

// Dependencies returns the order of the dependencies
func Dependencies(deps map[types.NamespacedName][]types.NamespacedName, res *[]types.NamespacedName, ks types.NamespacedName) {

	// if there is no deps then we stop
	if len(deps[ks]) == 0 {
		return
	}

	ksDeps := deps[ks]

	for _, k := range ksDeps {
		Dependencies(deps, res, k)
		*res = append(*res, k)
	}

}

func (c *Client) WaitForKs(ctx context.Context, kss ...types.NamespacedName) error {
	return c.fluxClient.WaitForKs(ctx, kss...)
}
