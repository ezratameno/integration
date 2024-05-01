package gitea

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	opts Opts
	do   *http.Client
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
