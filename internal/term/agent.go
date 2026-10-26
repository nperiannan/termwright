package term

import (
	"errors"
	"io"
	"net"
	"os"
	"runtime"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentSigners returns keys from a running SSH agent: $SSH_AUTH_SOCK on
// macOS/Linux, the Windows OpenSSH agent named pipe on Windows.
func agentSigners() []ssh.Signer {
	conn := agentConn()
	if conn == nil {
		return nil
	}
	// The connection must stay open while the signers are used during the
	// handshake; it is small and released when the process exits.
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return nil
	}
	return signers
}

// forwardAgent enables SSH agent forwarding for a session; smart card keys
// (cards) are offered next to the local agent's keys.
func forwardAgent(client *ssh.Client, sess *ssh.Session, cards []ssh.Signer) error {
	conn := agentConn()
	if conn == nil && len(cards) == 0 {
		return errors.New("no SSH agent is running")
	}
	var a agent.Agent
	if len(cards) > 0 {
		ca := &cardAgent{cards: cards}
		if conn != nil {
			ca.local = agent.NewClient(conn)
		}
		a = ca
	} else {
		a = agent.NewClient(conn)
	}
	if err := agent.ForwardToAgent(client, a); err != nil {
		return err
	}
	return agent.RequestAgentForwarding(sess)
}

func agentConn() io.ReadWriter {
	var conn io.ReadWriter
	if runtime.GOOS == "windows" {
		f, err := os.OpenFile(`\\.\pipe\openssh-ssh-agent`, os.O_RDWR, 0)
		if err != nil {
			return nil
		}
		conn = f
	} else {
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil
		}
		c, err := net.Dial("unix", sock)
		if err != nil {
			return nil
		}
		conn = c
	}
	return conn
}
