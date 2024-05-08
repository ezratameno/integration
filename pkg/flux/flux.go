package flux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ezratameno/integration/pkg/exec"
	helmv2 "github.com/fluxcd/helm-controller/api/v2beta2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	apimeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type Client struct {
	kubeClient client.Client
	out        io.Writer
}

func NewClient(out io.Writer) (*Client, error) {

	c := &Client{
		out: out,
	}

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
	log.SetLogger(logr.Logger{})

	// register the GitOps Toolkit schema definitions
	scheme := runtime.NewScheme()
	_ = sourcev1.AddToScheme(scheme)
	_ = helmv2.AddToScheme(scheme)
	_ = kustomizev1.AddToScheme(scheme)

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

	// fmt.Println(cmd)

	// Bootstrap flux
	var buf bytes.Buffer

	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Read from buffer until i get some string that indicate that resource was created
	go func() {

		defer cancel()
		for {
			time.Sleep(1 * time.Second)

			s := buf.String()

			if len(s) == 0 {
				continue
			}

			// fmt.Println(s)

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

	// TODO: maybe i need to use informers? or one time is enough?
	// Update the url
	gitRepo := &sourcev1.GitRepository{}
	c.kubeClient.Get(ctx, types.NamespacedName{
		Namespace: "flux-system",
		Name:      "flux-system",
	}, gitRepo)

	// TODO: improve this, maybe it's too specific?
	gitRepo.Spec.URL = opts.GitRepoUrl

	err = c.kubeClient.Patch(ctx, gitRepo, client.Merge, &client.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch changes: %w", err)
	}

	// Wait until git repo is in status ready

	fmt.Fprintf(c.out, "Waiting for git repo to be ready \n")
	err = wait.PollUntilContextCancel(ctx, 1*time.Second, true,
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

	return nil
}

// WaitForKs wait for the kustomization to be ready
func (c *Client) WaitForKs(ctx context.Context, kss ...types.NamespacedName) error {

	type Resp struct {
		err       error
		name      string
		namespace string
	}

	respCh := make(chan Resp)

	for _, ks := range kss {
		go func(client *Client, ks types.NamespacedName) {

			err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (done bool, err error) {
				namespacedName := types.NamespacedName{
					Namespace: ks.Namespace,
					Name:      ks.Name,
				}

				var ks kustomizev1.Kustomization

				if err := c.kubeClient.Get(ctx, namespacedName, &ks); err != nil {
					// Ignore not found error
					if apierrors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return meta.IsStatusConditionTrue(ks.Status.Conditions, apimeta.ReadyCondition), nil
			})

			respCh <- Resp{
				err:       err,
				name:      ks.Name,
				namespace: ks.Namespace,
			}

		}(c, ks)
	}

	for i := 0; i < len(kss); i++ {
		resp := <-respCh
		if resp.err != nil {
			return fmt.Errorf("failed to wait for ks %s in namespace %s: %w", resp.name, resp.namespace, resp.err)
		}

		fmt.Fprintf(c.out, "ks %s in ns %s is ready \n", resp.name, resp.namespace)
	}

	return nil
}

func (c *Client) ReconcileKS(ctx context.Context, kustomizations ...types.NamespacedName) error {

	type Resp struct {
		err       error
		name      string
		namespace string
	}

	respCh := make(chan Resp)

	for _, ks := range kustomizations {
		go func(c *Client, ks types.NamespacedName) {
			var kustomization kustomizev1.Kustomization

			err := c.kubeClient.Get(ctx, ks, &kustomization)
			if err != nil {

				respCh <- Resp{
					err:       fmt.Errorf("failed to get kustomization %s: %w", kustomization.Name, err),
					name:      ks.Name,
					namespace: ks.Namespace,
				}
				return
			}

			// Annotate the Kustomization resource to trigger reconciliation

			if len(kustomization.Annotations) == 0 {
				kustomization.Annotations = make(map[string]string)
			}
			kustomization.Annotations["reconcile.fluxcd.io/requestedAt"] = fmt.Sprintf("%d", time.Now().Unix())

			err = c.kubeClient.Patch(ctx, &kustomization, client.Merge)
			if err != nil {
				respCh <- Resp{
					err:       fmt.Errorf("failed to update kustomization %s: %w", kustomization.Name, err),
					name:      ks.Name,
					namespace: ks.Namespace,
				}
				return
			}

			respCh <- Resp{
				err:       nil,
				name:      ks.Name,
				namespace: ks.Namespace,
			}

		}(c, ks)
	}

	for i := 0; i < len(kustomizations); i++ {
		resp := <-respCh
		if resp.err != nil {
			return fmt.Errorf("failed to reconcile for ks %s in namespace %s: %w", resp.name, resp.namespace, resp.err)
		}

		fmt.Fprintf(c.out, "ks %s is ns %s is reconciled \n", resp.name, resp.namespace)
	}

	return nil
}
