package provider

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTunnelCommand is the client binary, assumed to be on PATH.
const DefaultTunnelCommand = "nivis-tunnel"

// DefaultProfile is the Nix profile an activation becomes a generation of.
//
// Generations are what make rollback possible at all, and deploy-rs's
// several-profiles-per-node model is where this eventually goes — which is why
// it is an attribute rather than a constant in the code.
const DefaultProfile = "/nix/var/nix/profiles/system"

// reachTimeout bounds how long an operation waits for a target to answer.
//
// A machine created moments ago is not yet reachable. Elastinix polls for
// exactly this reason, and a provider that gave up on the first refused
// connection would be unusable against a freshly created server — the only kind
// this project creates.
const reachTimeout = 3 * time.Minute

// reachInterval is how often to try again while waiting.
const reachInterval = 5 * time.Second

// target is everything needed to run a command on a machine through the tunnel.
type target struct {
	streamID      string
	relay         string
	keyFile       string
	tunnelCommand string
	sshExtraArgs  []string
}

// proxyCommand is the ssh ProxyCommand that routes the connection through the
// tunnel.
//
// This one string is the entire integration surface. ssh treats whatever the
// command puts on its stdio as the network, so nix-copy-closure and
// switch-to-configuration work over it with no knowledge of any of this.
func (t target) proxyCommand() string {
	return fmt.Sprintf("%s connect %s --relay %s --key %s",
		t.tunnelCommand, t.streamID, t.relay, t.keyFile)
}

// sshArgs builds the options shared by every command run on the target.
//
// accept-new rather than no. Under AWS SSM the tunnel authenticates the target,
// so ssh's host key check is redundant and elastinix disables it. This tunnel
// does not authenticate the target, so the check is doing real work: it pins on
// first contact and refuses an impostor on every connection after.
func (t target) sshArgs() []string {
	args := []string{
		"-o", "ProxyCommand=" + t.proxyCommand(),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=30",
	}
	return append(args, t.sshExtraArgs...)
}

// commandError carries what the target itself said.
//
// A generic "activation failed" is useless: the operator cannot see whether the
// closure was wrong, the disk was full, or a unit refused to start. The target's
// own stderr is the only thing that answers that.
type commandError struct {
	command string
	stderr  string
	err     error
}

func (e *commandError) Error() string {
	out := strings.TrimSpace(e.stderr)
	if out == "" {
		return fmt.Sprintf("%s: %v", e.command, e.err)
	}
	return fmt.Sprintf("%s: %v\n%s", e.command, e.err, out)
}

func (e *commandError) Unwrap() error { return e.err }

// run executes a command on the target and returns its stdout.
func (t target) run(ctx context.Context, remote string) (string, error) {
	args := append(t.sshArgs(), "root@"+t.streamID, remote)
	cmd := exec.CommandContext(ctx, "ssh", args...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", &commandError{command: remote, stderr: stderr.String(), err: err}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// sshOptsEnv renders the ssh options for NIX_SSHOPTS.
//
// NIX_SSHOPTS is a STRING that nix hands to a shell, not an argument vector, so
// it is word-split before ssh ever sees it. The ProxyCommand contains spaces —
// it is a whole command line — so joining the arguments naively hands ssh
// `-o ProxyCommand=nivis-tunnel` and then `connect` as a hostname, and the copy
// fails with "failed to start SSH connection" while the direct ssh calls, which
// pass a real argument vector, keep working. Each argument is quoted.
func (t target) sshOptsEnv() string {
	args := t.sshArgs()
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	return strings.Join(quoted, " ")
}

// shellQuote wraps a value in single quotes, escaping any it contains.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runLocal executes a command on the orchestrator with NIX_SSHOPTS set, so nix
// tooling reaches the target through the tunnel.
func (t target) runLocal(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(cmd.Environ(), "NIX_SSHOPTS="+t.sshOptsEnv())

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", &commandError{
			command: name + " " + strings.Join(args, " "),
			stderr:  stderr.String(),
			err:     err,
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// waitReachable retries a trivial command until the target answers.
func (t target) waitReachable(ctx context.Context) error {
	deadline := time.Now().Add(reachTimeout)

	var last error
	for {
		if _, err := t.run(ctx, "true"); err == nil {
			return nil
		} else {
			last = err
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("target %s did not become reachable within %s: %w",
				t.streamID, reachTimeout, last)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reachInterval):
		}
	}
}

// CurrentSystem reports the generation the target is actually running.
//
// Read from the machine rather than remembered from the last apply, which is
// the entire reason this is a resource and not a provisioner: a nixos-rebuild
// done by hand shows up as drift instead of being silently overwritten.
func (t target) CurrentSystem(ctx context.Context) (string, error) {
	return t.run(ctx, "readlink "+CurrentSystemLink)
}

// Activate performs the three steps, in order.
//
// Deliberately explicit rather than delegated to deploy-rs for now, so the
// proof of concept owns its failure modes. Adopting deploy-rs as the engine is
// a separate decision, recorded as nivis-tunnel-6f4m.
func (t target) Activate(ctx context.Context, closure, profile string) error {
	if err := t.waitReachable(ctx); err != nil {
		return err
	}

	// 1. Get the closure there. Only missing paths travel, which is what makes
	//    every deploy after the first small.
	if _, err := t.runLocal(ctx, "nix-copy-closure", "--to", "root@"+t.streamID, closure); err != nil {
		return fmt.Errorf("copying the closure: %w", err)
	}

	// 2. Make it a generation. This is what a rollback would later roll back to.
	if _, err := t.run(ctx, fmt.Sprintf("nix-env --profile %s --set %s", profile, closure)); err != nil {
		return fmt.Errorf("setting the profile: %w", err)
	}

	// 3. Make it live.
	if _, err := t.run(ctx, closure+"/bin/switch-to-configuration switch"); err != nil {
		return fmt.Errorf("switching to the new configuration: %w", err)
	}

	return nil
}
