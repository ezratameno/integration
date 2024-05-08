package integration

import (
	"fmt"
	"net"
	"path"
)

// Get preferred outbound ip of this machine
func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP, nil
}

func validateCreateOpts(opts *CreateOpts) error {
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
		return fmt.Errorf("local repo path is required")
	}

	// TODO: maybe generate a rand number so we can run multiple env?
	if opts.PrivateKeyPath == "" {
		opts.PrivateKeyPath = "/tmp/gitea-key.pem"
	}

	if path.Ext(opts.PrivateKeyPath) != ".pem" {
		return fmt.Errorf("private key path must be with pem extension")
	}

	if opts.KindClusterName == "" {
		opts.KindClusterName = "integration"
	}

	if opts.KindConfigPath == "" {
		return fmt.Errorf("kind config path is required")
	}

	return nil

}
