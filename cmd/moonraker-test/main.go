// Command moonraker-test is a CLI test harness for the moonraker client
// package. It connects to a Moonraker instance, subscribes to extruder
// and heater_bed objects, and prints every status update and gcode
// response to stdout.
//
// Usage:
//
//	go run ./cmd/moonraker-test -host <ip:port>
//	MOONRAKER_HOST=<ip:port> go run ./cmd/moonraker-test
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

func main() {
	host := flag.String("host", "", "Moonraker host:port (e.g. 192.168.1.100:7125)")
	flag.Parse()

	if *host == "" {
		*host = os.Getenv("MOONRAKER_HOST")
	}
	if *host == "" {
		fmt.Fprintln(os.Stderr, "Usage: moonraker-test -host <ip:port>")
		fmt.Fprintln(os.Stderr, "   or: MOONRAKER_HOST=<ip:port> moonraker-test")
		os.Exit(1)
	}

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Printf("→ Connecting to ws://%s/websocket …\n", *host)

	client, err := moonraker.New(*host)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	fmt.Println("✓ Connected")

	// Subscribe to extruder and heater_bed (all fields).
	fmt.Println("→ Subscribing to extruder, heater_bed …")
	initial, err := client.Subscribe(map[string][]string{
		"extruder":   nil, // all fields
		"heater_bed": nil, // all fields
	})
	if err != nil {
		log.Fatalf("Subscribe failed: %v", err)
	}

	fmt.Println("✓ Subscribed — initial state:")
	printStatusUpdate("INIT", initial)

	// Also do a one-shot query to demonstrate QueryObjects.
	fmt.Println("\n→ Querying toolhead position …")
	query, err := client.QueryObjects(map[string][]string{
		"toolhead": {"position", "homed_axes", "status"},
	})
	if err != nil {
		log.Printf("QueryObjects failed (non-fatal): %v", err)
	} else {
		printStatusUpdate("QUERY", query)
	}

	fmt.Println("\n→ Listening for updates (Ctrl+C to quit) …")
	fmt.Println("─────────────────────────────────────────────")

	// Graceful shutdown on SIGINT/SIGTERM.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case n := <-client.Notifications():
			switch n.Method {
			case moonraker.MethodStatusUpdate:
				if update, err := moonraker.ParseStatusUpdate(n.Params); err == nil {
					printStatusUpdate("UPDATE", &update)
				}
			case moonraker.MethodGcodeResponse:
				if resp, err := moonraker.ParseGcodeResponse(n.Params); err == nil {
					fmt.Printf("[GCODE] %s\n", resp)
				}
			default:
				fmt.Printf("[NOTIFY] %s\n", n.Method)
			}

		case s := <-sig:
			fmt.Printf("\n→ Caught %v, shutting down …\n", s)
			return
		}
	}
}

// printStatusUpdate pretty-prints a StatusUpdate to stdout.
func printStatusUpdate(tag string, u *moonraker.StatusUpdate) {
	for obj, fields := range u.Objects {
		data, _ := json.MarshalIndent(fields, "  ", "  ")
		fmt.Printf("[%s] %s:\n  %s\n", tag, obj, string(data))
	}
}
