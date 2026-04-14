# Architecture Design Document: MCP-Sentinel
**Status:** FINAL | **Target:** Production (v1.0) | **Core Language:** Go (1.22+)

## 1. Executive Summary
MCP-Sentinel is a zero-trust, transport-layer governance middleware designed to enforce deterministic policy and cryptographic state-snapshotting on Model Context Protocol (MCP) traffic. By intercepting JSON-RPC 2.0 streams and implementing an OS-level File Descriptor (FD) locking mechanism via the "Proxy-Execute" pattern, Sentinel mathematically eliminates Time-of-Check to Time-of-Use (TOCTOU) race conditions during Human-In-The-Loop (HITL) pauses. This architecture ensures FinTech-grade immutability for agentic workflows operating within local enterprise environments.

---

## 2. System Architecture Overview

The system operates as a transparent sidecar. It sits explicitly between the Agent Client (e.g., Claude Code CLI) and the downstream MCP Server or local host environment, hijacking the `stdio` transport.

### 2.1 High-Level Component Interaction
1.  **Client (Agent):** Initiates JSON-RPC `tools/call` or `tools/list`.
2.  **Transport Interceptor (Go):** Multiplexes `stdin`/`stdout` and performs MITM schema injection.
3.  **Semantic Policy Engine:** Evaluates tool intent against `config.yaml` using binary deterministic routing.
4.  **Lock & Execute Manager:** Extracts parameters via the internal Registry, acquires `syscall.Flock`, handles HITL, and performs the Proxy-Execute.
5.  **Flight Recorder (CAS):** Commits SHA-256 snapshots to local SQLite via Write-Ahead Logging.

---

## 3. Core Component Design (Go Implementation)

### 3.1 The Transport Interceptor (`/pkg/transport`)
The proxy handles standard input/output streams without dropping bytes or causing the AI agent to timeout during prolonged HITL pauses.

* **Mechanics:** Utilizes `bufio.Scanner` to read JSON-RPC messages separated by standard `

` headers. Spawns dedicated goroutines for bidirectional traffic.
* **MITM Schema Injection:** When the agent initializes via `tools/list`, the proxy intercepts the MCP server's response. It unmarshals the JSON array, dynamically injects Sentinel's synthetic compliance tools (e.g., `sentinel_check_status`), and remarshals the payload. This guarantees the LLM recognizes the polling fallback tools without schema violations.
* **Protocol Compliance (Keep-Alives):** * If the client passes `_meta.progressToken`, the proxy utilizes standard `$/progress` notifications during a HITL pause.
    * If no token is present, the proxy utilizes a **Polling Fallback**. It returns an immediate synthetic error (`-32001: PENDING_APPROVAL`) containing a `polling_id`, instructing the agent to queue the task and use the injected `sentinel_check_status` tool to periodically verify authorization.

### 3.2 Semantic Policy Engine (`/pkg/policy`)
Responsible for evaluating the JSON-RPC payload against enterprise rules using strict binary routing to prevent arbitrary execution loopholes.

* **Configuration:** Loaded via a strict `yaml` decoder at startup.
* **Deterministic Routing (e.g., `write_file`):** Routed to the Proxy-Execute Lock Manager. Target files can be successfully locked at the OS level.
* **Non-Deterministic Routing (e.g., `bash_command`):** Routed to the Strict Shell Gateway. Defaults to `DENY`. If permitted via HITL, human reviewers receive a prominent state-drift warning, as arbitrary shell targets cannot be cryptographically locked.
* **Heuristic Redaction Engine:** Replaces heavy AST parsing. Utilizes compiled Go `regexp` to strip base64 payloads, collapse massive arrays, and remove multi-line boilerplate from `read_file` responses, ensuring the binary remains lightweight while aggressively reducing agent token costs.

### 3.3 Lock & Execute Manager (`/pkg/governance`)
This is the core differentiator that neutralizes the TOCTOU vulnerability.

* **The Parameter Mapping Registry:** A hardcoded Go map (`map[string]string`) that links tool names to their respective JSON-RPC target path fields (e.g., `write_file` -> `params.arguments.path`). This allows the manager to dynamically extract the correct file to lock using rapid JSON traversal (e.g., `tidwall/gjson`).
* **The OS Lock:** Uses Go's `syscall.Flock(fd, syscall.LOCK_EX)` on POSIX systems (or `LockFileEx` on Windows).
* **The Proxy-Execute Pattern:** Instead of passing a locked file descriptor to an external MCP server, the Go binary temporarily assumes the role of the environment. Upon HITL approval, Sentinel performs the `os.WriteFile` operation itself, releases the lock, and returns a synthetic successful JSON-RPC response to the agent.
* **Safety Guarantees:** Utilizes `defer` aggressively. If the proxy process is killed (`SIGKILL`), the OS kernel automatically drops the file descriptors, preventing permanent system deadlocks.

### 3.4 Flight Recorder (Content-Addressed Storage) (`/pkg/storage`)
The cryptographic audit trail designed for nanosecond hot-path insertions.

* **Database (OLTP):** Utilizes `modernc.org/sqlite` (100% pure Go). This entirely eliminates CGO toolchain dependencies, ensuring cross-platform static compilation. Configured with `PRAGMA journal_mode=WAL` for high concurrency.
* **Hashing:** Implements `crypto/sha256` for all environment diffs.
* **Data Export (OLAP):** Includes a standalone `sentinel export --format=parquet` command. This separates operational storage from analytical querying, allowing enterprise teams to dump historical SQLite data into Snowflake or Datadog for compliance audits.

### 3.5 Local HITL Server & UI (`/pkg/ui`)
A lightweight, local HTTP server (`net/http`) bound to `127.0.0.1:8080` that serves a high-fidelity visual dashboard to prevent human alert fatigue.

* **Mechanism:** Flagged payloads are pushed to a Go channel (`chan *HitlRequest`). The HTTP server consumes this channel and renders the proposed state mutation.
* **Visual Diffing Specifications:** * *No Raw JSON dumps.* The UI must instantly communicate Intent, Context, and Blast Radius.
    * *Vertical Stacking Layout:* Metadata (Tool, Target Path, Risk Level) is pinned at the top. The "Before State" code block is stacked directly above the "After State" code block.
    * *Action Resolution:* Clear `Approve Execution` and `Deny & Abort` buttons. Handlers immediately resolve the pending Go channel, unblocking the Lock Manager to proceed with the Proxy-Execute or synthesize an MCP error.

---

## 4. Critical Path: Execution Flow (Write Operation)

1.  **Ingress:** Agent writes `{"jsonrpc": "2.0", "method": "tools/call", "params": {"name": "write_file", "arguments": {"path": ".aws/credentials", "content": "..."}}}` to `stdout`.
2.  **Intercept:** `mcp-mux` reads the payload.
3.  **Evaluate:** `sentinel-pep` matches `.aws/credentials` to a `HITL` policy.
4.  **Lock Phase:**
    * Registry identifies target path. `lock-smith` opens `.aws/credentials`.
    * Executes `syscall.Flock(fd, LOCK_EX)`. The file is now kernel-locked.
5.  **Snapshot Phase:**
    * `flight-recorder` reads the file content, generates `hash_pre`.
    * Inserts pending record into SQLite.
6.  **Pause Phase:**
    * Goroutine blocks on `<-hitlApprovalChannel`.
    * `mcp-mux` injects polling error fallback or standard progress notifications.
7.  **Resolution Phase:**
    * Human reviews `hash_pre` via the vertical visual diff on `127.0.0.1:8080` and clicks Approve.
8.  **Execution Phase (Proxy-Execute):**
    * `lock-smith` writes the new content to the locked FD.
    * `flight-recorder` generates `hash_post`, updates SQLite record.
    * `lock-smith` executes `syscall.Flock(fd, LOCK_UN)` and closes the FD.
9.  **Egress:** `mcp-mux` sends synthetic JSON-RPC success response to the Agent.

---

## 5. Deployment & Distribution
To maximize the "viral" adoption metric for EB-1A Original Contribution requirements:
* **Compilation:** Distributed as a single, statically linked Go binary via GitHub Actions (`GOOS=linux GOARCH=arm64 go build`). 
* **Installation:** A zero-friction script (`curl -sSfL https://mcp-sentinel.sh | sh`) drops the binary into `/usr/local/bin/sentinel`.
* **Integration:** Engineers prefix existing agent commands: `sentinel run claude-code`.
