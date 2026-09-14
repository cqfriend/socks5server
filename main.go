package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"socks5/pkg"
)

var (
	address  string
	username string
	password string
	udp      string
	showVer  bool

	// These are set by the build script with -ldflags.
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func init() {
	flag.StringVar(&address, "a", ":1080", "listen on the address")
	flag.StringVar(&username, "u", "", "username")
	flag.StringVar(&password, "p", "", "password")
	flag.StringVar(&udp, "udp", "all", "udp mode: all, associate, uot, off")
	flag.BoolVar(&showVer, "v", false, "show version information")
	flag.Usage = usage
	flag.Parse()
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `socks5 - a SOCKS5 proxy server with full TCP/BIND/UDP support.

Usage:
  %s [options]

Options:
`, os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(out, `
UDP modes (-udp):
  all        accept the UDP ASSOCIATE command and UDP over TCP (default)
  associate  accept the UDP ASSOCIATE command only
  uot        accept UDP over TCP only (v1 and v2)
  off        reject all UDP requests

UDP over TCP:
  As a client, tunnel UDP over TCP by sending CONNECT to one of the magic addresses:
    v1: sp.udp-over-tcp.arpa
    v2: sp.v2.udp-over-tcp.arpa

Examples:
  %s -a :1080
  %s -a :1080 -u user -p pass -udp uot
`, os.Args[0], os.Args[0])
}

func main() {
	if showVer {
		fmt.Printf("socks5 %s (commit %s, built %s by %s)\n", version, commit, date, builtBy)
		return
	}

	mode, err := parseUDPMode(udp)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}

	logger := log.New(os.Stderr, "[socks5] ", log.LstdFlags)
	svc := &socks5.Server{
		Logger: logger,
		UDP:    mode,
	}
	if username != "" {
		svc.Authentication = socks5.UserAuth(username, password)
	}
	err = svc.ListenAndServe("tcp", address)
	if err != nil {
		logger.Println(err)
	}
}

func parseUDPMode(name string) (socks5.UDPMode, error) {
	switch strings.ToLower(name) {
	case "all", "":
		return socks5.UDPAll, nil
	case "associate":
		return socks5.UDPAssociateOnly, nil
	case "uot":
		return socks5.UDPOverTCPOnly, nil
	case "off", "none", "disable", "disabled":
		return socks5.UDPDisabled, nil
	default:
		return socks5.UDPAll, fmt.Errorf("invalid -udp value %q, expected one of: all, associate, uot, off", name)
	}
}
