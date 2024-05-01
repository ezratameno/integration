package gitea

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"code.gitea.io/sdk/gitea"
)

type Opts struct {
	SSHPort  int
	HttpPort int
	Addr     string

	adminUser     string
	adminPassword string
	adminEmail    string
}

type Client struct {
	opts   Opts
	do     *http.Client
	client *gitea.Client
}

func NewClient(opts Opts) *Client {
	c := &Client{
		opts: opts,
		do: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}

	return c
}

type SignUpOpts struct {
	Email    string
	Password string
	Username string
}

func (c *Client) Start(ctx context.Context) error {

	// TODO: 1. install gitea (docker + actual installment)

	// 2. sign up, the first user that signs up is admin

	signupOpts := SignUpOpts{
		Email:    "admin@gmail.com",
		Password: "adminlabuser",
		Username: "labuser",
	}
	err := c.Signup(ctx, signupOpts)
	if err != nil {
		return fmt.Errorf("failed signing user: %w", err)
	}

	// Set up admin information
	c.opts.adminEmail = signupOpts.Email
	c.opts.adminUser = signupOpts.Username
	c.opts.adminPassword = signupOpts.Password

	// TODO: create gitea client

	client, err := gitea.NewClient(fmt.Sprintf("%s:%d", c.opts.Addr, c.opts.HttpPort),
		gitea.SetBasicAuth(c.opts.adminUser, c.opts.adminPassword))
	if err != nil {
		return fmt.Errorf("failed to create gitea client: %w", err)
	}

	c.client = client

	return nil
}

func (c *Client) Signup(ctx context.Context, opts SignUpOpts) error {

	signUpurl := fmt.Sprintf("%s:%d/user/sign_up", c.opts.Addr, c.opts.HttpPort)

	u, err := url.Parse(signUpurl)
	if err != nil {
		return err
	}

	// Add form data

	data := url.Values{}
	data.Set("user_name", opts.Username)
	data.Set("email", opts.Email)
	data.Set("password", opts.Password)
	data.Set("retype", opts.Password)

	// Encode the form data
	reqBody := bytes.NewBufferString(data.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), reqBody)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "lang=en-US; i_like_gitea=8e2779a79e7d3e28; _csrf=uBwdvQ2EKSS69kVzPIGOPI1OmoU6MTU5NDMxMTk2NzA1ODIxMjgzNw")

	resp, err := c.do.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GeneratePrivatePublicKeys will generate a public and private key in gitea.
// the user will pass the path to where to save the private key.
func (c *Client) GeneratePrivatePublicKeys(publicKeyName string, privateKeyPath string) (*gitea.PublicKey, error) {

	privateKey, err := rsa.GenerateKey(rand.Reader, 3071)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Extract the public key from the private key
	publicKey := privateKey.Public()

	// Convert the public key
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, err
	}

	// Encode the public key to Pem format
	publicKeyPem := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	// Create public key in gitea
	pubKey, _, err := c.client.CreatePublicKey(gitea.CreateKeyOption{
		Title: publicKeyName,
		Key:   string(publicKeyPem),
	})

	if err != nil {
		return nil, err
	}

	// TODO: do i need to supply a function that delete the key in case of failure?

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	// Encode the public key to PEM format
	privateKeyPem := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	// Save the private key to file
	err = os.WriteFile(privateKeyPath, privateKeyPem, 0744)
	if err != nil {
		return pubKey, fmt.Errorf("failed to save private key to file: %w", err)
	}

	return pubKey, nil
}

// CreateRepoFromExisting creates a repo and copies all the files from the location
func (c *Client) CreateRepoFromExisting(opts gitea.CreateRepoOption, filesLocation string) (*gitea.Repository, error) {

	repo, _, err := c.client.CreateRepo(opts)
	if err != nil {
		return nil, err
	}

	// Copy all the files from the location to the gitea repo
	err = filepath.WalkDir(filesLocation, func(path string, d fs.DirEntry, err error) error {

		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// skip on .git files
		if strings.Contains(path, ".git") {
			return nil
		}

		fmt.Println("path", path)

		fileLoc := strings.TrimPrefix(path+"/", filesLocation)

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _, err = c.client.CreateFile(c.opts.adminUser, repo.Name, fileLoc, gitea.CreateFileOptions{
			Content: base64.StdEncoding.EncodeToString(body),
		})

		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return nil, nil
}
