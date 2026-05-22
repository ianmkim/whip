// Package sshx dials hosts using ~/.ssh/config + ssh-agent, the same way the
// `ssh` binary would. Keys, ProxyJump, etc. are honored to whatever extent
// the underlying libraries support.
package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Dial connects to the given Host alias as it would be resolved from
// ~/.ssh/config. The returned client should be Closed by the caller.
func Dial(alias string) (*ssh.Client, error) {
	host := ssh_config.Get(alias, "HostName")
	if host == "" {
		host = alias
	}
	port := ssh_config.Get(alias, "Port")
	if port == "" {
		port = "22"
	}
	username := ssh_config.Get(alias, "User")
	if username == "" {
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
	}

	auth, err := buildAuth(alias)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec — TUI use, ad-hoc trust model
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(host, port)
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("invalid port %q for %s", port, alias)
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s (%s@%s): %w", alias, username, addr, err)
	}
	return client, nil
}

func buildAuth(alias string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	for _, path := range ssh_config.GetAll(alias, "IdentityFile") {
		expanded := expand(path)
		key, err := os.ReadFile(expanded)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			continue // encrypted; agent should cover it
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if len(methods) == 0 {
		return nil, errors.New("no SSH auth available (start ssh-agent or configure IdentityFile)")
	}
	return methods, nil
}

func expand(p string) string {
	if len(p) > 1 && p[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
