// Command demo-backends runs the twin fake APIs used by the README,
// the examples, and scripts/smoke.sh: backend A plays the legacy
// service, backend B plays the rewrite. B carries four intentional
// regressions (a renamed enum value, a dropped field, a stringified
// count, a lost route) plus the usual noise (fresh request ids and
// timestamps, different Server headers) so every twinget feature has
// something real to chew on.
//
// Usage:
//
//	go run ./examples/demo-backends [--port-a N] [--port-b N]
//
// Ports default to 0 (ephemeral). The chosen base URLs are printed to
// stdout as "A=…" and "B=…" lines, then the process serves until
// interrupted. Both listeners bind 127.0.0.1 only.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// user is one record in the demo user list.
type user struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Email     string `json:"email,omitempty"`
	CreatedAt string `json:"created_at"`
}

// requestID fabricates a fresh UUID-shaped id per response — exactly
// the noise --ignore-ids exists for.
func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// now renders the clock the way each stack would: the legacy Node
// service emits toISOString() milliseconds, the Go rewrite emits plain
// RFC 3339 — so the two sides always differ textually, like real life.
func now(legacy bool) string {
	if legacy {
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// writeJSON emits a payload with the given extra headers.
func writeJSON(w http.ResponseWriter, status int, contentType string, v any) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Request-Id", requestID())
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// users returns the shared demo dataset; role and email vary by side
// to model the rewrite's regressions.
func users(legacy bool, limit int) []user {
	role := "administrator"
	if legacy {
		role = "admin"
	}
	list := []user{
		{
			ID:        "7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f",
			Name:      "Aiko",
			Role:      role,
			CreatedAt: "2026-05-01T09:00:00Z",
		},
		{
			ID:        "0d9e8f7a-6b5c-4d3e-2f1a-0b9c8d7e6f5a",
			Name:      "Ben",
			Role:      "member",
			CreatedAt: "2026-06-15T14:30:00Z",
		},
	}
	if legacy {
		list[1].Email = "ben@example.test" // the rewrite forgot this field
	}
	if limit > 0 && limit < len(list) {
		list = list[:limit]
	}
	return list
}

// makeHandler builds one backend. legacy=true is backend A.
func makeHandler(legacy bool) http.Handler {
	server := "go-rewrite/2.0"
	contentType := "application/json"
	if legacy {
		server = "legacy-node/14.21"
		contentType = "application/json; charset=utf-8"
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list := users(legacy, limit)
		// Regression: the rewrite stringifies the count.
		var total any = len(list)
		if !legacy {
			total = strconv.Itoa(len(list))
		}
		w.Header().Set("Server", server)
		writeJSON(w, http.StatusOK, contentType, map[string]any{
			"users":        list,
			"total":        total,
			"generated_at": now(legacy),
			"request_id":   requestID(),
		})
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		uptime := 863125 // the legacy box has been up for ten days
		if !legacy {
			uptime = 4217 // the rewrite deployed an hour ago
		}
		w.Header().Set("Server", server)
		writeJSON(w, http.StatusOK, contentType, map[string]any{
			"status":     "ok",
			"uptime_s":   uptime,
			"checked_at": now(legacy),
		})
	})

	mux.HandleFunc("/api/orders/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", server)
		if !legacy {
			// Regression: the rewrite lost this route.
			writeJSON(w, http.StatusNotFound, contentType, map[string]any{
				"error": "order not found",
			})
			return
		}
		writeJSON(w, http.StatusOK, contentType, map[string]any{
			"order_id": 42,
			"sku":      "TWG-0042",
			"state":    "shipped",
			"total":    "129.90",
			"placed":   "2026-07-01T08:15:00Z",
		})
	})

	return mux
}

// listenAndAnnounce binds 127.0.0.1:port, prints "NAME=http://…", and
// serves in the background.
func listenAndAnnounce(name string, port int, h http.Handler) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	fmt.Printf("%s=http://%s\n", name, ln.Addr().String())
	go func() {
		_ = http.Serve(ln, h)
	}()
	return nil
}

func main() {
	portA := flag.Int("port-a", 0, "port for backend A (0 = ephemeral)")
	portB := flag.Int("port-b", 0, "port for backend B (0 = ephemeral)")
	flag.Parse()

	if err := listenAndAnnounce("A", *portA, makeHandler(true)); err != nil {
		fmt.Fprintln(os.Stderr, "demo-backends:", err)
		os.Exit(1)
	}
	if err := listenAndAnnounce("B", *portB, makeHandler(false)); err != nil {
		fmt.Fprintln(os.Stderr, "demo-backends:", err)
		os.Exit(1)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
}
