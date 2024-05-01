package flux

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ezratameno/integration/pkg/exec"
	helmv2 "github.com/fluxcd/helm-controller/api/v2beta2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

type Opts struct {
	PrivateKeyPath string
	Branch         string
	Path           string
	Password       string
	Username       string
	Url            string
}

type Client struct {
	opts       Opts
	kubeClient client.Client
}

func NewClient(opts Opts) (*Client, error) {

	// register the GitOps Toolkit schema definitions
	scheme := runtime.NewScheme()
	_ = sourcev1.AddToScheme(scheme)
	_ = helmv2.AddToScheme(scheme)

	// init Kubernetes client
	kubeClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil
	}

	c := &Client{
		opts: opts,
	}

	c.kubeClient = kubeClient

	return c, nil
}

func (c *Client) Bootstrap(ctx context.Context) error {

	cmd := fmt.Sprintf(`flux bootstrap git --url="ssh://git@%s" --branch="%s" --private-key-file="%s" --path="%s" --password="%s" --username="%s" --token-auth=true`,
		c.opts.Url, c.opts.Branch, c.opts.PrivateKeyPath, c.opts.Path, c.opts.Password, c.opts.Username)

	fmt.Println(cmd)

	// Bootstrap flux
	var buf bytes.Buffer

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// TODO: handle better
	err := exec.LocalExecContext(cmdCtx, cmd, &buf)
	if err != nil {
		fmt.Println(err)
	}

	// Update the url
	gitRepo := &sourcev1.GitRepository{}
	c.kubeClient.Get(ctx, types.NamespacedName{
		Namespace: "flux-system",
		Name:      "flux-system",
	}, gitRepo)

	ip, err := GetOutboundIP()
	if err != nil {
		return err
	}

	// TODO: improve this
	gitRepo.Spec.URL = fmt.Sprintf("http://%s:3000/labuser/test.git", ip.String())

	fmt.Println("url", gitRepo.Spec.URL)

	err = c.kubeClient.Patch(ctx, gitRepo, client.Merge, &client.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch changes: %w", err)
	}

	// TODO: make sure all kustomizations are running ok? (reconcile?)

	return nil
}

// Get preferred outbound ip of this machine
func GetOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP, nil
}
