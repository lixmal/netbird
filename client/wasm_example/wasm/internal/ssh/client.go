package ssh

import (
	"context"
	"fmt"
	"io"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type Client struct {
	nbClient  *netbird.Client
	sshClient *ssh.Client
	session   *ssh.Session
	stdin     io.WriteCloser
	stdout    io.Reader
	stderr    io.Reader
}

// NewClient creates a new SSH client
func NewClient(nbClient *netbird.Client) *Client {
	return &Client{
		nbClient: nbClient,
	}
}

// Connect establishes an SSH connection through NetBird network
func (c *Client) Connect(host string, port int, username string) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	logrus.Infof("SSH: Connecting to %s as %s", addr, username)

	authMethods := []ssh.AuthMethod{}

	sshKeyPEM := c.nbClient.GetSSHKey()
	if sshKeyPEM == "" {
		return fmt.Errorf("no NetBird SSH key available - key should be generated during client initialization")
	}

	logrus.Debugf("SSH: Key length: %d bytes", len(sshKeyPEM))
	if len(sshKeyPEM) > 100 {
		logrus.Debugf("SSH: Key preview: %s...", sshKeyPEM[:100])
	} else {
		logrus.Debugf("SSH: Full key: %s", sshKeyPEM)
	}

	signer, err := parseSSHPrivateKey([]byte(sshKeyPEM))
	if err != nil {
		return fmt.Errorf("parse NetBird SSH private key: %w", err)
	}

	pubKey := signer.PublicKey()
	logrus.Infof("SSH: Using NetBird key authentication with public key type: %s", pubKey.Type())

	authMethods = append(authMethods, ssh.PublicKeys(signer))

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := c.nbClient.Dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SSH handshake: %w", err)
	}

	c.sshClient = ssh.NewClient(sshConn, chans, reqs)
	logrus.Infof("SSH: Connected to %s", addr)

	return nil
}

// StartSession starts an SSH session with PTY
func (c *Client) StartSession(cols, rows int) error {
	if c.sshClient == nil {
		return fmt.Errorf("SSH client not connected")
	}

	session, err := c.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	c.session = session

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
		ssh.VINTR:         3,
		ssh.VQUIT:         28,
		ssh.VERASE:        127,
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		return fmt.Errorf("PTY request: %w", err)
	}

	c.stdin, err = session.StdinPipe()
	if err != nil {
		session.Close()
		return fmt.Errorf("get stdin: %w", err)
	}

	c.stdout, err = session.StdoutPipe()
	if err != nil {
		session.Close()
		return fmt.Errorf("get stdout: %w", err)
	}

	c.stderr, err = session.StderrPipe()
	if err != nil {
		session.Close()
		return fmt.Errorf("get stderr: %w", err)
	}

	if err := session.Shell(); err != nil {
		session.Close()
		return fmt.Errorf("start shell: %w", err)
	}

	logrus.Info("SSH: Session started with PTY")
	return nil
}

// Write sends data to the SSH session
func (c *Client) Write(data []byte) (int, error) {
	if c.stdin == nil {
		return 0, fmt.Errorf("SSH session not started")
	}
	return c.stdin.Write(data)
}

// Read reads data from the SSH session
func (c *Client) Read(buffer []byte) (int, error) {
	if c.stdout == nil {
		return 0, fmt.Errorf("SSH session not started")
	}
	return c.stdout.Read(buffer)
}

// Resize updates the terminal size
func (c *Client) Resize(cols, rows int) error {
	if c.session == nil {
		return fmt.Errorf("SSH session not started")
	}
	return c.session.WindowChange(rows, cols)
}

// Close closes the SSH connection
func (c *Client) Close() error {
	if c.session != nil {
		c.session.Close()
	}
	if c.sshClient != nil {
		return c.sshClient.Close()
	}
	return nil
}
