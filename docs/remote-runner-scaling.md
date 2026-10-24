# Scaling Remote Runners

This document analyzes the scalability boundaries of Kestrix's remote runner architecture and outlines concrete architectural evolution paths for managing large fleets of distributed workers.

---

## 1. Current Architecture: Long-Polling HTTP

Today, remote runners use an outbound-only HTTP long-polling model:

```
┌────────────────────────────────────────┐
│              Kestrix Host              │
│                                        │
│  ┌──────────────┐     ┌─────────────┐  │
│  │ Postgres DB  │     │ HTTP Server │  │
│  │(remote_jobs) │     │ (:8080)     │  │
│  └──────┬───────┘     └──────▲──────┘  │
└─────────┼────────────────────┼─────────┘
          │ SKIP LOCKED        │ GET /api/runner-jobs/next (timeout=30s)
          ▼                    │ PATCH .../trace
     [Atomic Claim]            │ PUT .../complete
                               │
               ┌───────────────┴───────────────┐
               │         NAT / Firewall        │
               └───────────────┬───────────────┘
                               │
                ┌──────────────┴──────────────┐
                │        Remote Runner        │
                │ (kestrix runner --token ..) │
                └─────────────────────────────┘
```

### Why this design works well now:
- **Zero Ingress Requirements:** Runners live behind NAT, corporate VPNs, or home networks with no public IP or firewall ports.
- **Transactional Safety:** Job claiming relies directly on PostgreSQL's `SELECT ... FOR UPDATE SKIP LOCKED`, preventing race conditions across concurrent workers.
- **Protocol Simplicity:** Standard HTTP/1.1 semantics make debugging with curl, proxies, and observability tools straightforward.
- **Poll Limiter (`runnerPollLimiter`):** Hard bounds concurrency per runner token to prevent runaway poll loops.

---

## 2. Scale Bottlenecks at High Fleet Counts

When scaling to dozens or hundreds of concurrent remote runners (e.g., dynamic Kubernetes runner autoscaling, bare-metal clusters), several bottlenecks emerge:

### A. Connection & Goroutine Overhead
- Each idle runner holds an open HTTP connection for up to 30 seconds (`GET /api/runner-jobs/next`).
- 500 runners idling = 500 open TCP sockets and 500 server goroutines blocked in poll sleep loops.
- Reverse proxies (Nginx, Envoy, Cloudflare, Traefik) and load balancers have connection limits and aggressive timeout dropouts.

### B. Database Query Pressure (Poll Jitter & Polling Intervals)
- When no jobs are pending, polling runners periodically query the `remote_jobs` table (even with backoff, dozens of queries per second hit Postgres).
- A burst of 100 simultaneous jobs finishing or starting causes concurrent `SKIP LOCKED` queries, creating lock contention on the index.

### C. Streaming Trace Logs
- Runners periodically push log chunks via `PATCH /api/runner-jobs/{id}/trace`.
- With hundreds of active jobs emitting verbose logs (e.g., debug compilers, container pulls), the HTTP server handles high write traffic that must be appended to database rows or broadcasted to frontend subscribers.

---

## 3. Future Architectural Evolution Paths

### Option A: Event-Driven Postgres Push (`LISTEN / NOTIFY`)
Instead of each poll loop issuing periodic SQL queries while waiting:
1. When a new job is created in `remote_jobs`, a Postgres trigger or application service executes `NOTIFY remote_jobs_channel, '<tags_json>'`.
2. The Kestrix server maintains a single listening connection (`pgxpool` / `pq.Listener`).
3. Waiting runner HTTP handler goroutines register against an in-memory broadcast channel and sleep until notified.
4. **Benefit:** Reduces database polling query volume to near-zero during idle periods. Simple change without altering runner protocol.

### Option B: Bidirectional gRPC / HTTP/2 Stream Control Plane
Replace the repeated request/response polling with a long-lived multiplexed gRPC stream:

```
Runner                                                Kestrix Server
  │                                                         │
  ├────── Open Stream: rpc StreamControl(RunnerHello) ─────>│
  │                                                         │ (Maintains persistent stream)
  │<───── JobOffer(job_id, tags, epoch) ────────────────────┤
  ├────── JobAccept(job_id) ───────────────────────────────>│ (Atomic claim in DB)
  │                                                         │
  ├────── StreamTrace(chunk, offset) ──────────────────────>│ (Pipelined log chunks)
  ├────── Heartbeat() ─────────────────────────────────────>│ (Lightweight ping)
  │                                                         │
  ├────── JobComplete(result) ─────────────────────────────>│
```

- **Multiplexing:** Single TCP connection per runner handles job dispatch, heartbeats, trace streaming, and cancellations.
- **Instant Dispatch:** Sub-millisecond latency from job enqueue to runner receipt (no waiting for next poll cycle).
- **Graceful Backpressure:** Server signals runner when overloaded without dropping connections.

### Option C: Relay / Proxy Node Architecture
For geographically distributed runners or edge environments:
- Lightweight Kestrix runner edge relay caches git source archives locally and aggregates runner connections before forwarding to the central Kestrix control plane.
- Prevents cross-region source archive download latency.

---

## 4. Phased Roadmap

| Phase | Scope | Impact | Complexity |
|-------|-------|--------|------------|
| **Phase 1** | In-memory Pub/Sub + Postgres `LISTEN/NOTIFY` for long-poll wakeups | Eliminates periodic DB polling during idle times; 10x capacity on same Postgres | Low |
| **Phase 2** | Chunked trace buffer in memory before DB flush | Reduces database write IOPS by 80% during intense builds | Medium |
| **Phase 3** | gRPC / WebSocket control stream alternative | Sub-second dispatch, single connection per runner fleet | High |
