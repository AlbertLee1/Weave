package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

func runAuth(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave auth <login|logout|status>")
		return 2
	}
	switch args[0] {
	case "login":
		return authLogin(args[1:], stdout, stderr)
	case "logout":
		return authLogout(args[1:], stdout, stderr)
	case "status":
		return authStatus(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave auth: unknown subcommand %q\n", args[0])
		return 2
	}
}

func authLogin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "user email (required)")
	password := fs.String("password", "", "user password (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" || *password == "" {
		fmt.Fprintln(stderr, "weave: --email and --password are required")
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	resp, err := c.Login(context.Background(), *email, *password)
	if err != nil {
		fmt.Fprintf(stderr, "login: %v\n", err)
		return 1
	}
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	cfg.AccessToken = resp.AccessToken
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(stderr, "save config: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Logged in as %s (token expires in %d seconds).\n", resp.User.Email, resp.ExpiresIn)
	return 0
}

func authLogout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	// Best-effort: tell the server, but ignore failures so a stale local
	// token can still be cleared.
	if cfg.BaseURL != "" {
		if c, code := newCLIClient(stderr); c != nil {
			_ = c.Logout(context.Background(), "")
			_ = code
		}
	}
	cfg.AccessToken = ""
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(stderr, "save config: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Logged out.")
	return 0
}

func authStatus(args []string, stdout, stderr io.Writer) int {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	if cfg.Token() == "" {
		fmt.Fprintln(stdout, "not logged in (no token configured)")
		return 0
	}
	fmt.Fprintf(stdout, "logged in (token set, base_url=%s)\n", cfg.BaseURL)
	return 0
}
