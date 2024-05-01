package kind

import (
	"fmt"
	"os"
	"path"

	"sigs.k8s.io/kind/pkg/cluster"
)

const (
	defaultConfigPath = ""
)

type Client struct {
	p *cluster.Provider
}

func NewClient() *Client {
	p := cluster.NewProvider(cluster.ProviderWithDocker())

	c := &Client{
		p: p,
	}

	return c
}

func (c *Client) CreateClusterWithConfig(name string, configPath string) error {
	return c.p.Create(name, cluster.CreateWithConfigFile(configPath))
}

func (c *Client) DeleteCluster(name string) error {

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home dir: %w", err)
	}

	return c.p.Delete(name, path.Join(home, ".kube", "config"))
}
