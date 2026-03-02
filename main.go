// muxd CLI entry point
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/batalabs/muxd/internal/agent"
	"github.com/batalabs/muxd/internal/checkpoint"
	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/hub"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/service"
	"github.com/batalabs/muxd/internal/store"
	"github.com/batalabs/muxd/internal/tui"
)

var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
}

func main() {
	versionFlag := flag.Bool("version", false, "Print version and exit")
	modelFlag := flag.String("model", "", "Model name or alias (e.g. claude-sonnet, openai/gpt-4o)")
	continueFlag := flag.String("c", "", "Resume a session (latest for cwd, or pass a session ID)")
	daemonFlag := flag.Bool("daemon", false, "Run in daemon mode (no TUI)")
	bindFlag := flag.String("bind", "", "Network interface to bind (localhost, 0.0.0.0, or specific IP)")
	hubFlag := flag.Bool("hub", false, "Run as hub coordinator (no agent/session machinery)")
	hubBindFlag := flag.String("hub-bind", "", "Hub bind address (default: localhost)")
	hubInfoFlag := flag.Bool("hub-info", false, "Print hub connection info (token, address, QR) and exit")
	remoteFlag := flag.String("remote", "", "Connect to remote daemon or hub (host:port)")
	tokenFlag := flag.String("token", "", "Auth token for remote connection")
	serviceCmd := flag.String("service", "", "Service management: install|uninstall|status|start|stop")
	flag.Parse()

	// Set up log file — all stderr output is also written to ~/.local/share/muxd/muxd.log.
	logger := config.NewLogger()
	defer logger.Close()

	if *versionFlag {
		fmt.Printf("muxd %s\n", version)
		return
	}

	// Print hub connection info from lockfile
	if *hubInfoFlag {
		printHubInfo()
		return
	}

	// Handle service commands first (no store/API key needed)
	if *serviceCmd != "" {
		if err := service.HandleCommand(*serviceCmd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	prefs := config.LoadPreferences()

	// Hub-only mode: start hub server, no agent/session machinery
	if *hubFlag {
		hubDB, err := hub.OpenHubStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening hub database: %v\n", err)
			os.Exit(1)
		}
		defer hubDB.Close()

		h := hub.NewHub(hubDB, &prefs, logger)
		saveHubTokenIfNew(&prefs, h.AuthToken())

		hubBind := *hubBindFlag
		if hubBind == "" {
			hubBind = prefs.HubBindAddress
		}
		if hubBind != "" {
			h.SetBindAddress(hubBind)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := h.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "hub: shutdown: %v\n", err)
			}
		}()

		if err := h.Start(4097); err != nil {
			fmt.Fprintf(os.Stderr, "hub error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Remote TUI mode: connect to a remote daemon or hub
	if *remoteFlag != "" {
		baseURL := "http://" + *remoteFlag
		dc := daemon.NewDaemonClient(0)
		dc.SetBaseURL(baseURL)
		dc.SetAuthToken(*tokenFlag)

		info, err := dc.HealthCheck()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot reach remote %s: %v\n", *remoteFlag, err)
			os.Exit(1)
		}

		modelLabel := *modelFlag
		if modelLabel == "" {
			modelLabel = prefs.Model
		}
		var modelID string
		if modelLabel != "" {
			_, modelID = provider.ResolveProviderAndModel(modelLabel, prefs.Provider)
		}

		resetTerminalForTUI()

		if info.Mode == "hub" {
			// Hub mode: launch TUI with node picker, no session yet
			fmt.Fprintf(os.Stderr, "Connected to hub on %s\n", *remoteFlag)
			m := tui.InitialModel(dc, version, modelLabel, modelID, nil, nil, false, nil, prefs, "")
			m.SetHubConnection(baseURL, *tokenFlag)
			p := tea.NewProgram(m)
			tui.SetProgram(p)
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "muxd failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Direct daemon: create/resume session remotely
			fmt.Fprintf(os.Stderr, "Connected to remote daemon on %s (%s/%s)\n", *remoteFlag, info.Provider, info.Model)
			cwd := mustGetwd()
			sessionID, createErr := dc.CreateSession(cwd, modelID)
			if createErr != nil {
				fmt.Fprintf(os.Stderr, "error creating session: %v\n", createErr)
				os.Exit(1)
			}
			session, err := dc.GetSession(sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error loading session: %v\n", err)
				os.Exit(1)
			}
			p := tea.NewProgram(tui.InitialModel(dc, version, modelLabel, modelID, nil, session, false, nil, prefs, ""))
			tui.SetProgram(p)
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "muxd failed: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	provider.SetOllamaBaseURL(prefs.OllamaURL)

	// Resolve provider and model (no hardcoded default — user must configure)
	modelLabel := *modelFlag
	if modelLabel == "" {
		modelLabel = prefs.Model
	}

	var providerName, modelID, apiKey string
	var prov provider.Provider

	if modelLabel != "" {
		providerName, modelID = provider.ResolveProviderAndModel(modelLabel, prefs.Provider)
		apiKey, _ = config.LoadProviderAPIKey(prefs, providerName)
		if p, err := provider.GetProvider(providerName); err == nil {
			prov = p
		} else {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}

	provider.SetPricingMap(config.LoadPricing())

	st, err := store.OpenStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	// Agent factory for the daemon server
	agentFactory := func(key, mID, mLabel string, s *store.Store, sess *domain.Session, p provider.Provider) *agent.Service {
		return agent.NewService(key, mID, mLabel, s, sess, p)
	}

	// Resolve bind address from flag or preferences
	bindAddr := *bindFlag
	if bindAddr == "" {
		bindAddr = prefs.DaemonBindAddress
	}
	if bindAddr == "" {
		bindAddr = "localhost" // secure default
	}

	// Daemon-only mode: start HTTP server, no TUI
	if *daemonFlag {
		srv := daemon.NewServer(st, apiKey, modelID, modelLabel, prov, &prefs)
		saveAuthTokenIfNew(&prefs, srv.AuthToken())
		srv.SetAgentFactory(agentFactory)
		srv.SetDetectGitRepo(checkpoint.DetectGitRepo)
		srv.SetBindAddress(bindAddr)
		srv.SetLogger(logger)

		// Handle graceful shutdown
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		// Node auto-registration with hub (if configured)
		var hubClient *hub.NodeClient
		var hubNodeID string
		if prefs.HubURL != "" && prefs.HubClientToken != "" {
			hubClient = hub.NewNodeClient(prefs.HubURL, prefs.HubClientToken, srv.AuthToken())
			go func() {
				port := srv.Port() // blocks until listener is bound
				name := prefs.HubClientName
				if name == "" {
					hostname, _ := os.Hostname()
					name = hostname
				}
				nodeID, err := hubClient.Register(name, bindAddr, port, version)
				if err != nil {
					fmt.Fprintf(os.Stderr, "hub: registration failed: %v\n", err)
					return
				}
				hubNodeID = nodeID
				fmt.Fprintf(os.Stderr, "hub: registered as client %s\n", nodeID)

				// Start heartbeat loop
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						if err := hubClient.Heartbeat(nodeID); err != nil {
							fmt.Fprintf(os.Stderr, "hub: heartbeat failed: %v\n", err)
						}
					case <-ctx.Done():
						return
					}
				}
			}()
		}

		go func() {
			<-ctx.Done()
			// Deregister from hub before shutting down
			if hubClient != nil && hubNodeID != "" {
				if err := hubClient.Deregister(hubNodeID); err != nil {
					fmt.Fprintf(os.Stderr, "hub: deregister failed: %v\n", err)
				}
			}
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: shutdown: %v\n", err)
			}
		}()

		if err := srv.Start(4096); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// TUI mode: check for existing daemon
	var dc *daemon.DaemonClient
	var embeddedServer *daemon.Server

	lf, lfErr := daemon.ReadLockfile()
	if lfErr == nil && !daemon.IsLockfileStale(lf) {
		// Connect to existing daemon
		dc = daemon.NewDaemonClient(lf.Port)
		dc.SetAuthToken(lf.Token)
		if info, err := dc.HealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: daemon on port %d not responding: %v\n", lf.Port, err)
			fmt.Fprintf(os.Stderr, "hint: kill the old process and restart muxd\n")
		} else if info.Provider == "" || info.Model == "" {
			fmt.Fprintf(os.Stderr, "Connected to daemon on port %d (pid %d) — no model configured\n", lf.Port, info.PID)
			fmt.Fprintf(os.Stderr, "hint: the daemon was started without a provider/model.\n")
			fmt.Fprintf(os.Stderr, "      kill the process (pid %d) and restart: muxd --daemon\n", info.PID)
		} else {
			fmt.Fprintf(os.Stderr, "Connected to daemon on port %d (%s/%s)\n", lf.Port, info.Provider, info.Model)
		}
	} else {
		// Start embedded server
		embeddedServer = daemon.NewServer(st, apiKey, modelID, modelLabel, prov, &prefs)
		saveAuthTokenIfNew(&prefs, embeddedServer.AuthToken())
		embeddedServer.SetAgentFactory(agentFactory)
		embeddedServer.SetDetectGitRepo(checkpoint.DetectGitRepo)
		embeddedServer.SetQuiet(true)
		embeddedServer.SetBindAddress(bindAddr)
		embeddedServer.SetLogger(logger)
		go func() {
			if err := embeddedServer.Start(4096); err != nil {
				fmt.Fprintf(os.Stderr, "embedded server error: %v\n", err)
			}
		}()
		// Port() blocks until Start() has bound the listener, so no race.
		dc = daemon.NewDaemonClient(embeddedServer.Port())
		dc.SetAuthToken(embeddedServer.AuthToken())

		// Hub registration for embedded server (same as daemon mode)
		if prefs.HubURL != "" && prefs.HubClientToken != "" {
			hubClient := hub.NewNodeClient(prefs.HubURL, prefs.HubClientToken, embeddedServer.AuthToken())
			go func() {
				port := embeddedServer.Port()
				name := prefs.HubClientName
				if name == "" {
					hostname, _ := os.Hostname()
					name = hostname
				}
				nodeID, err := hubClient.Register(name, bindAddr, port, version)
				if err != nil {
					fmt.Fprintf(os.Stderr, "hub: registration failed: %v\n", err)
					return
				}
				fmt.Fprintf(os.Stderr, "hub: registered as client %s\n", nodeID)

				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					if err := hubClient.Heartbeat(nodeID); err != nil {
						fmt.Fprintf(os.Stderr, "hub: heartbeat failed: %v\n", err)
					}
				}
			}()
		}
	}

	// Create or resume session
	cwd := mustGetwd()
	var session *domain.Session
	resuming := false

	if *continueFlag != "" {
		// -c <id> → resume specific session
		session, err = st.GetSession(*continueFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session not found: %v\n", err)
			os.Exit(1)
		}
		resuming = true
	} else if flag.NArg() == 0 {
		// Check if "-c" appeared in os.Args with no value
		for _, arg := range os.Args[1:] {
			if arg == "-c" {
				session, err = st.LatestSession(cwd)
				if err == sql.ErrNoRows {
					fmt.Fprintf(os.Stderr, "no sessions found for %s\n", cwd)
					os.Exit(1)
				} else if err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					os.Exit(1)
				}
				resuming = true
				break
			}
		}
	}

	if session == nil {
		// Create session via daemon
		sessionID, createErr := dc.CreateSession(cwd, modelID)
		if createErr != nil {
			fmt.Fprintf(os.Stderr, "error creating session: %v\n", createErr)
			os.Exit(1)
		}
		session, err = st.GetSession(sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading session: %v\n", err)
			os.Exit(1)
		}
	}

	// Ensure the first TUI frame starts from a clean terminal state.
	resetTerminalForTUI()

	p := tea.NewProgram(tui.InitialModel(dc, version, modelLabel, modelID, st, session, resuming, prov, prefs, apiKey))
	tui.SetProgram(p)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "muxd failed: %v\n", err)
		os.Exit(1)
	}

	// Cleanup embedded server
	if embeddedServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		if err := embeddedServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "embedded server: shutdown: %v\n", err)
		}
	}
}

func resetTerminalForTUI() {
	// Start the TUI on a fresh line without terminal control sequences.
	// This avoids prompt-line overlap issues on some Windows terminals.
	fmt.Println()
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// saveAuthTokenIfNew persists the daemon auth token to preferences if it
// wasn't already saved. This is called from main (not inside NewServer)
// to avoid writing a partial prefs struct before the caller is ready.
func saveAuthTokenIfNew(prefs *config.Preferences, token string) {
	if prefs.DaemonAuthToken == token {
		return
	}
	prefs.DaemonAuthToken = token
	if err := config.SavePreferences(*prefs); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save auth token: %v\n", err)
	}
}

// saveHubTokenIfNew persists the hub auth token to preferences.
func saveHubTokenIfNew(prefs *config.Preferences, token string) {
	if prefs.HubAuthToken == token {
		return
	}
	prefs.HubAuthToken = token
	if err := config.SavePreferences(*prefs); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save hub auth token: %v\n", err)
	}
}

// printHubInfo reads the hub lockfile and prints connection info + QR code.
func printHubInfo() {
	lf, err := hub.ReadHubLockfile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "No running hub found: %v\n", err)
		os.Exit(1)
	}

	// Determine display host — use LAN IP when bound to all interfaces
	host := lf.BindAddr
	if host == "0.0.0.0" || host == "" || host == "localhost" {
		if ips := daemon.GetLocalIPs(); len(ips) > 0 {
			host = ips[0]
		}
	}

	ascii, err := daemon.GenerateQRCodeASCII(host, lf.Port, lf.Token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "QR generation failed: %v\n", err)
	} else {
		fmt.Printf("\n%s\n", ascii)
	}

	fmt.Printf("  hub:   %s:%d\n", host, lf.Port)
	fmt.Printf("  token: %s\n", lf.Token)
	fmt.Printf("\n  connect: muxd --remote %s:%d --token %s\n\n", host, lf.Port, lf.Token)
}
