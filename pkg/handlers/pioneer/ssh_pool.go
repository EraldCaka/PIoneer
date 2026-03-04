package pioneer

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

type sshPool struct {
	mu         sync.Mutex
	cfg        *config.Config
	sem        chan struct{}
	reconnects atomic.Int64
	log        *zap.Logger
}

func newSSHPool(cfg *config.Config, log *zap.Logger) (*sshPool, error) {
	p := &sshPool{
		cfg: cfg,
		sem: make(chan struct{}, cfg.PoolSize),
		log: log,
	}
	for i := 0; i < cfg.PoolSize; i++ {
		p.sem <- struct{}{}
	}

	if _, err := p.Run("echo connected"); err != nil {
		return nil, fmt.Errorf("failed to initialize SSH pool: %v", err)
	}

	log.Info("SSH pool initialized",
		zap.Int("size", cfg.PoolSize),
		zap.String("url", cfg.Url),
		zap.String("auth", string(cfg.AuthMethod)),
	)
	return p, nil
}

func (p *sshPool) Run(command string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(p.cfg.RetryDelay) * time.Second)
			p.log.Warn("retrying SSH command",
				zap.String("command", command),
				zap.Int("attempt", attempt),
				zap.Error(lastErr),
			)
			p.reconnects.Add(1)
		}

		<-p.sem
		out, err := p.exec(command)
		p.sem <- struct{}{}

		if err != nil {
			lastErr = err
			continue
		}
		return out, nil
	}
	return "", fmt.Errorf("command failed after %d retries: %v", p.cfg.MaxRetries, lastErr)
}

func (p *sshPool) exec(command string) (string, error) {
	target := fmt.Sprintf("pi@%s", p.cfg.Url)

	var cmd *exec.Cmd
	switch p.cfg.AuthMethod {
	case config.AuthKey:
		cmd = exec.Command("ssh",
			"-i", p.cfg.SSHKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "BatchMode=yes",
			"-p", p.cfg.Port,
			target,
			command,
		)
	default: // password
		cmd = exec.Command("sshpass",
			"-p", p.cfg.Password,
			"ssh",
			"-o", "StrictHostKeyChecking=no",
			"-p", p.cfg.Port,
			target,
			command,
		)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("SSH exec failed: %v, stderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (p *sshPool) Close() {
	p.log.Info("SSH pool closed")
}

func (p *sshPool) Size() int {
	return p.cfg.PoolSize
}
