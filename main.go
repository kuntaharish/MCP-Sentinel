// main.go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// SentinelProxy represents the core transport interceptor
type SentinelProxy struct {
	logger *log.Logger
	// Channels for routing JSON-RPC payloads
	agentToServer chan []byte
	serverToAgent chan []byte
}

func NewSentinelProxy() *SentinelProxy {
	return &SentinelProxy{
		logger:        log.New(os.Stderr, "[MCP-SENTINEL] ", log.LstdFlags|log.Lmsgprefix),
		agentToServer: make(chan []byte, 100),
		serverToAgent: make(chan []byte, 100),
	}
}

func (p *SentinelProxy) Start(ctx context.Context) error {
	p.logger.Println("Initializing Zero-Trust Transport Interceptor...")
	
	var wg sync.WaitGroup

	// 1. Intercept Standard Input (Agent -> Proxy)
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.readStream(ctx, os.Stdin, p.agentToServer, "Agent")
	}()

	// 2. Intercept Standard Output (Server -> Proxy)
	// Note: In a real deployment where the proxy wraps a downstream server, 
	// this would read from the child process's stdout. 
	// For now, we mock the bidirectional flow.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Placeholder for downstream server stdout reader
	}()

	// 3. The Governance Event Loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.processRoutings(ctx)
	}()

	p.logger.Println("Transport layer active. Awaiting JSON-RPC payloads.")
	wg.Wait()
	return nil
}

// Replace the readStream function in main.go with this:

func (p *SentinelProxy) readStream(ctx context.Context, reader *os.File, outChan chan<- []byte, source string) {
	scanner := bufio.NewScanner(reader)
	
	// OVERRIDE: Use our strict MCP Protocol parser instead of default line-scanning
	scanner.Split(transport.MCPSplitFunc)
	
	// CRITICAL: MCP payloads can be massive (e.g., full file reads). 
	// Expand the scanner buffer to 10MB to prevent 'token too long' panics.
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			payload := scanner.Bytes()
			
			// Deep copy is mandatory before passing to the channel 
			// to prevent the scanner from overwriting the memory address on the next tick.
			safePayload := make([]byte, len(payload))
			copy(safePayload, payload)
			outChan <- safePayload
		}
	}

	if err := scanner.Err(); err != nil {
		p.logger.Printf("FATAL: Stream read error from %s: %v", source, err)
	}
}

// processRoutings acts as the central brain, passing payloads to the Semantic Policy Engine
func (p *SentinelProxy) processRoutings(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			p.logger.Println("Shutting down routing loop.")
			return
		case payload := <-p.agentToServer:
			// TODO: Route to Semantic Policy Engine -> Lock Manager -> Flight Recorder
			p.logger.Printf("INTERCEPTED payload: %s", string(payload))
			
			// Mock pass-through to stdout (Agent expects response on stdout)
			fmt.Fprintf(os.Stdout, "%s\n", payload) 
		}
	}
}

func main() {
	// Setup graceful shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n[MCP-SENTINEL] Received shutdown signal. Releasing OS locks and terminating.")
		cancel()
	}()

	proxy := NewSentinelProxy()
	if err := proxy.Start(ctx); err != nil {
		log.Fatalf("Fatal proxy error: %v", err)
	}
}