//go:build windows || (darwin && !ios)

package cmd

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	vncserver "github.com/netbirdio/netbird/client/vnc/server"
)

var (
	vncAgentSocket    string
	vncAgentTargetUID uint32

	// vncAgentStop cancels the agent's own context so a platform hook can end
	// the process through the normal shutdown path rather than os.Exit.
	vncAgentStopMu sync.Mutex
	vncAgentStop   context.CancelFunc
)

func init() {
	vncAgentCmd.Flags().StringVar(&vncAgentSocket, "socket", "", "Unix-domain socket path the agent listens on (required)")
	vncAgentCmd.Flags().Uint32Var(&vncAgentTargetUID, "target-uid", 0, "uid the agent should drop privileges to before listening (darwin only; 0 = stay as current uid)")
	rootCmd.AddCommand(vncAgentCmd)
}

// vncAgentCmd runs a VNC server inside the user's interactive session,
// listening on a Unix-domain socket. The NetBird service spawns it: on
// Windows via CreateProcessAsUser into the console session, on macOS via
// launchctl asuser into the Aqua session.
var vncAgentCmd = &cobra.Command{
	Use:    "vnc-agent",
	Short:  "Run VNC capture agent (internal, spawned by service)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.SetReportCaller(true)
		log.SetFormatter(&log.JSONFormatter{})
		log.SetOutput(os.Stderr)

		if vncAgentSocket == "" {
			return fmt.Errorf("--socket is required")
		}

		token := os.Getenv("NB_VNC_AGENT_TOKEN")
		if token == "" {
			return fmt.Errorf("NB_VNC_AGENT_TOKEN not set; agent requires a token from the service")
		}
		// Purge the token from env so it doesn't leak via /proc/<pid>/environ.
		if err := os.Unsetenv("NB_VNC_AGENT_TOKEN"); err != nil {
			log.Debugf("unset NB_VNC_AGENT_TOKEN: %v", err)
		}

		// Drop root privileges to the target console user BEFORE creating
		// the listening socket: keeps a post-auth bug in the encoder /
		// input / capture paths confined to the user's own privileges
		// rather than escalating to host root, and makes the daemon's
		// LOCAL_PEERCRED check see the right uid. No-op on Windows
		// (both processes run as SYSTEM) and when --target-uid is 0.
		if vncAgentTargetUID != 0 {
			if err := dropAgentPrivileges(vncAgentTargetUID); err != nil {
				return fmt.Errorf("drop privileges to uid %d: %w", vncAgentTargetUID, err)
			}
		}

		if err := os.Remove(vncAgentSocket); err != nil && !os.IsNotExist(err) {
			log.Debugf("remove stale socket %s: %v", vncAgentSocket, err)
		}
		ln, err := net.Listen("unix", vncAgentSocket)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", vncAgentSocket, err)
		}
		if err := os.Chmod(vncAgentSocket, 0o600); err != nil {
			log.Debugf("chmod %s: %v", vncAgentSocket, err)
		}

		ctx, stop := context.WithCancel(cmd.Context())
		defer stop()
		vncAgentStopMu.Lock()
		vncAgentStop = stop
		vncAgentStopMu.Unlock()

		capturer, injector, err := newAgentResources()
		if err != nil {
			_ = ln.Close()
			return err
		}
		srv := vncserver.New(vncserver.Config{
			Capturer:      capturer,
			Injector:      injector,
			DisableAuth:   true,
			AgentTokenHex: token,
			Listener:      ln,
		})

		if err := srv.Start(ctx, netip.AddrPort{}, netip.Prefix{}); err != nil {
			return fmt.Errorf("start vnc server: %w", err)
		}
		log.Infof("vnc-agent listening on %s, ready", vncAgentSocket)

		<-ctx.Done()
		log.Info("vnc-agent context cancelled, shutting down")
		return srv.Stop()
	},
	SilenceUsage: true,
}

// vncAgentGiveUp ends the agent through its own context, so the server stops
// cleanly and the service spawns a replacement on the next connection.
func vncAgentGiveUp() {
	vncAgentStopMu.Lock()
	stop := vncAgentStop
	vncAgentStopMu.Unlock()
	if stop != nil {
		stop()
	}
}
