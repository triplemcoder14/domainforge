package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

type Config struct {
	QulesAdmin   string `json:"qules_admin"`
	AdminAddress string `json:"admin_address"`
}

func defaultConfig() *Config {
	return &Config{
		QulesAdmin:   "http://localhost:1990",
		AdminAddress: "localhost:2013",
	}
}

func getConfigDir() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}

	var configDir string
	switch runtime.GOOS {
	case "windows":
		configDir = filepath.Join(home, "AppData", "Roaming", "domainforge")
	case "darwin":
		configDir = filepath.Join(home, "Library", "Application Support", "domainforge")
	default:
		configDir = filepath.Join(home, ".config", "domainforge")
	}

	return configDir, nil
}

func saveConfig(cfg *Config) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configFile := filepath.Join(configDir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
}

func readConfig() (*Config, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return &Config{}, err
	}

	configFile := filepath.Join(configDir, "config.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return &Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &Config{}, err
	}

	return &cfg, nil
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip != nil && !ip.IsLoopback() && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no suitable local IP address found")
}

func run(cfg *Config) {
	df := NewDomainForge()

	listener, err := net.Listen("tcp", cfg.AdminAddress)
	if err != nil {
		log.Fatalf("failed to start domainforge server: %v", err)
	}
	defer listener.Close()

	log.Println("domainForge server started. listening on", cfg.AdminAddress)

	ctx, cancel := context.WithCancel(context.Background())

	go df.startBroadcast(ctx)

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		cancel()
	}()

	doneChan := make(chan struct{})
	connections := make(chan net.Conn)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("error accepting connection: %v\n", err)
					continue
				}
			}

			select {
			case connections <- conn:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case conn := <-connections:
			go handleConnection(doneChan, conn, df)
		case <-doneChan:
			cancel()
		case <-ctx.Done():
			log.Println("shutting down domainforge")
			df.Shutdown()
			return
		}
	}
}

func handleConnection(ch chan struct{}, conn net.Conn, df *DomainForge) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		cmd := parts[0]
		switch cmd {
		case "add":
			if len(parts) != 4 || parts[2] != "--port" {
				fmt.Fprintln(conn, "Invalid command. Usage: add <domain> --port <port>")
				return
			}
			domain := parts[1]
			port, err := strconv.Atoi(parts[3])
			if err != nil {
				fmt.Fprintf(conn, "Invalid port number: %v\n", err)
				return
			}
			err = df.Add(domain, port)
			if err != nil {
				fmt.Fprintf(conn, "Error: %v\n", err)
			} else {
				fmt.Fprintf(conn, "Added domain: %s with port: %d\n", domain, port)
			}
		case "remove":
			if len(parts) != 2 {
				fmt.Fprintln(conn, "Invalid command. Usage: remove <domain>")
				return
			}
			domain := parts[1]
			err := df.Remove(domain)
			if err != nil {
				fmt.Fprintf(conn, "Error: %v\n", err)
			} else {
				fmt.Fprintf(conn, "Removed domain: %s\n", domain)
			}
		case "list":
			domains := df.List()
			if len(domains) == 0 {
				fmt.Fprintln(conn, "No domains registered")
			} else {
				fmt.Fprintln(conn, "Registered domains:")
				for _, domain := range domains {
					fmt.Fprintf(conn, "- %s\n", domain)
				}
			}
		case "stop":
			close(ch)
		default:
			fmt.Fprintln(conn, "Unknown command")
		}
	}
}

func sendCommand(command string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	conn, err := net.Dial("tcp", cfg.AdminAddress)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %v", err)
	}
	defer conn.Close()

	_, err = fmt.Fprintln(conn, command)
	if err != nil {
		return fmt.Errorf("failed to send command: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading response: %v", err)
	}

	return nil
}

var rootCmd = &cobra.Command{
	Use:   "domainforge",
	Short: "domainForge is a tool for managing local domains",
	Long:  `domainForge enables you to handle local domains along with their associated ports.`,
}

var addCmd = &cobra.Command{
	Use:   "add <domain> --port <port>",
	Short: "add a new domain",
	Long:  `Add a new domain to DomainForge with the specified port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("usage: domainforge add <domain> --port <port>")
		}
		port, _ := cmd.Flags().GetInt("port")
		if port == 0 {
			return fmt.Errorf("port is required")
		}
		return sendCommand(fmt.Sprintf("add %s --port %d", args[0], port))
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "start the domainforge",
	Long:  `Start the domainforge, either in the foreground or as a detached process.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		qulesAdmin, _ := cmd.Flags().GetString("qules")
		adminAddr, _ := cmd.Flags().GetInt("addr")
		detached, _ := cmd.Flags().GetBool("detached")

		cfg := &Config{
			AdminAddress: fmt.Sprintf(":%d", adminAddr),
			QulesAdmin:   qulesAdmin,
		}

		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %v", err)
		}

		if detached {
			cmd := exec.Command(os.Args[0], "start")
			cmd.Stdout = nil
			cmd.Stderr = nil
			cmd.Stdin = nil
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := cmd.Start(); err != nil {
				return fmt.Errorf("failed to start in detached mode: %v", err)
			}

			return nil
		}

		run(cfg)
		return nil
	},
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop domainforge daemon",
		Long:  `Stop the running domainforge daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendCommand("stop")
		},
	}
}

func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <domain>",
		Short: "Remove a domain",
		Long:  `Remove a domain from DomainForge.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: domainforge remove <domain>")
			}
			return sendCommand(fmt.Sprintf("remove %s", args[0]))
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all domains",
		Long:  `List all domains registered in DomainForge.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendCommand("list")
		},
	}
}

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().IntP("port", "p", 0, "port for the .local domain")
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().IntP("addr", "a", 2013, "domainforge process address")
	startCmd.Flags().StringP("qules", "c", "http://localhost:1990", "local qules admin address")
	startCmd.Flags().BoolP("detached", "d", false, "run domainforge in background")
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(removeCmd())
	rootCmd.AddCommand(listCmd())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("[domainforge]: %v", err)
	}
}
