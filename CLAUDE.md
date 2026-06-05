# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

EVDI is a self-developed Virtual Desktop Infrastructure (VDI) cloud desktop platform. The project is currently in the **architecture planning phase** — design documents in `docs/` contain full technology selection, architecture, and API specifications (written in Chinese).

## Repository Structure

```
docs/
├── 01-整体架构设计.md           # Technology selection & seven-layer architecture
├── 02-Broker控制平面架构设计.md # Broker sub-services, domain model, API spec, DB schema
├── 03-Agent架构设计.md          # Agent (Pion WebRTC + GStreamer) architecture
└── 架构图.png                   # Architecture diagram
```

No source code exists yet. When code is added, it will follow this planned layout:

```
evdi-broker/       # L3 Broker — Go, REST API on :8080, WebSocket signaling
evdi-agent/        # L2 Streaming Agent — Go, gRPC on :50052, Pion + GStreamer
evdi-web-client/   # L1 Web Client — React 18, TypeScript, Vite, port 3000
evdi-tauri-client/ # L1 Native Client — Rust (Tauri) + TypeScript frontend
```

## Build & Development Commands

### Broker (`evdi-broker/`)
```bash
go build ./cmd/broker/     # Build
go test ./... -v -coverprofile=coverage.out  # Test all
go test ./pkg/scheduler/ -run TestSchedule -v  # Test single
go run ./cmd/broker/       # Dev server (port 8080)
```

### Streaming Agent (`evdi-agent/`)
```bash
make build-linux           # Cross-compile for Linux (CGO_ENABLED=1)
make build-windows         # Cross-compile for Windows (mingw)
make proto                 # Generate gRPC code from proto/evdi/agent/v1/agent.proto
make test                  # go test ./... -v -coverprofile=coverage.out
make test-single PKG=./pkg/pipeline/ TEST=TestPipelineStart
```

### Web Client (`evdi-web-client/`)
```bash
npm run dev                # Vite dev server (port 3000, proxies to Broker :8080)
npm run build              # tsc && vite build
npm test                   # Vitest
npm run test:coverage      # Vitest with 80%+ coverage target
```

## Seven-Layer Architecture

```
L1  Client Layer        — Web client (HTML5 Canvas) + Tauri Native client (Rust)
L2  Media & Access      — Agent (Pion WebRTC + GStreamer) + Coturn (TURN)
L3  Business Orchestration — Broker (6 sub-services) + Console
L4  Desktop Delivery    — Linux container Pods + Windows VMs (KubeVirt)
L5  K8s Workloads       — KubeVirt · Longhorn · Kube-OVN · Prometheus Operator
L6  K8s Orchestration   — Kubernetes control plane
L7  Infrastructure      — Physical servers, network, storage
```

Data flows bottom-up: L7 hardware → L6 K8s → L5 operators → L4 desktop instances → L3 business APIs → L2 media streaming → L1 client rendering.

## Broker Sub-Services

The Broker (L3) is split into 6 sub-services by change frequency, scaling dimension, and fault isolation:

| Sub-service | Responsibility | Scaling |
|---|---|---|
| **Desktop Service** | Desktop & session lifecycle, user/tenant/quota/policy CRUD, REST API (`/api/v1/`) | By API QPS |
| **Scheduler Service** | Resource pool/cluster/node selection, consumes NATS `schedule.request`, REST callback | By scheduling concurrency |
| **Gateway Service** | WebSocket signaling, JWT/session token issuance, TURN credential distribution, Redis Pub/Sub sync | By WebSocket connections (min 3 replicas) |
| **Monitor Service** | Agent heartbeat, state consistency check, business metrics export | By agent count |
| **Event Center** | Alert routing → notification/ticket/self-heal/Kagent, consumes Alertmanager webhooks | By alert volume |
| **Audit Service** | Async audit log write (NATS → PostgreSQL), monthly partitioned tables | By write volume |

**Communication patterns:**
- **Synchronous REST**: Client-facing APIs, scheduler callback to Desktop Service
- **Asynchronous NATS JetStream**: State change events, audit logs, alert events (topics: `schedule.*`, `alert.*`, `audit.*`)

## Core Domain Model

```
Tenant
├── TenantPolicy (one per tenant)
├── ResourcePool (CPU/GPU/Dedicated/AI)
└── User
    ├── UserQuota (one per user)
    └── DesktopInstance
        ├── DesktopTemplate (CPU/Memory/GPU/Image/Storage)
        ├── DesktopPolicy (optional override, inherits TenantPolicy)
        └── Session (1:N, only one Connected at a time)
```

**Three-layer state model:**
- `DesktopState`: Assigned → Provisioning → Starting → Initializing → Ready → Stopping → Stopped (+ Error/Recovering)
- `SessionState`: Created → Connecting → Connected → Disconnected → Closed
- `UsageState`: Available / Occupied / Inactive

Key invariant: `VM Running ≠ Ready` — Ready is a business state, requires Agent to report all fields true.

## Agent Architecture (L2)

Agent runs inside each desktop instance (Pod/VM). Components:
- **Pion WebRTC engine**: Lite ICE mode (server-side), H.264 video + Opus audio, DataChannel for keyboard/mouse/clipboard
- **GStreamer encoding engine**: Dynamic GPU detection (NVIDIA NVENC → Intel/AMD VAAPI → x264 software fallback), runs as separate process for LGPL compliance
- **Broker communication**: gRPC client, heartbeat push, config pull
- **Session management**: Connection state machine with exponential backoff reconnect

Agent interacts with only two external components: Client (WebRTC inbound) and Broker (gRPC outbound).

## Key Design Decisions

- **Unified K8s plane**: Both Linux containers and Windows VMs are K8s Pods — same API, same scheduler, same RBAC.
- **GPU passthrough**: KubeVirt CRD declares host-passthrough; used for Windows VMs and hardware encoding.
- **Longhorn block storage**: 3-replica strong consistency, data-locality reads for boot storm mitigation.
- **Kube-OVN VLAN bridging**: Desktops get enterprise LAN IPs on boot — no NAT mapping needed. Multi-tenant L2 isolation via Subnet + ACL.
- **Pion WebRTC**: Go-native, same language as K8s controllers and Broker. Media + DataChannel multiplexed on one connection.
- **GStreamer**: Cross-vendor hardware encoding in one pipeline. Process isolation + dynamic linking for LGPL compliance.
- **Tauri client**: <15 MB package. Rust backend for system-level hooks (keyboard intercept, USBIP redirection).
- **NATS JetStream**: Lightweight message bus for inter-service async communication, K8s-native.
- **PostgreSQL**: All business data, soft delete, audit logs partitioned by month (180-day retention).
- **Redis**: WebSocket multi-replica sync (Pub/Sub), token blacklist, session cache.
- **Kagent integration**: AI Agent for ops automation, accesses Broker exclusively via MCP Server — never directly touches DB or K8s API.

## API Conventions

All REST endpoints follow `https://<broker>/api/v1/` with envelope response:
```json
{ "code": 0, "message": "success", "data": { } }
```

- **Auth**: JWT (RS256), Access Token 30min, silent refresh 5min before expiry
- **WebSocket signaling**: `wss://<broker>/api/v1/signal?token=<sessionToken>`
- **Pagination**: `?page=1&pageSize=20`, response includes `items`, `total`, `page`, `pageSize`
- **Time format**: ISO 8601 UTC (`"2024-01-01T08:00:00Z"`)
- **IDs**: UUID v4, application-generated
- **Multi-device exclusion**: New session kicks old session, pushes `SESSION_REPLACED` event

## Multi-Tenant Isolation

Four layers of isolation:
1. **Compute**: K8s Namespace per tenant (`vdi-tenant-{tenant_id}`) + ResourceQuota
2. **Network**: Kube-OVN Subnet per tenant + ACL cross-tenant deny
3. **Data**: PostgreSQL `tenant_id` column on all tables, enforced at query level from JWT
4. **Business**: RBAC (super_admin / tenant_admin / user)

## License Awareness

The product is intended for closed-source distribution:
- **Apache-2.0, MIT, BSD**: Safe for direct linking.
- **LGPL-2.1 (GStreamer)**: Must use process isolation + dynamic linking — never statically link into proprietary binaries.
- **GPL**: Avoid entirely unless isolated behind a network service boundary.

## Implementation Notes

When implementing components:
- **L3 Broker**: Go, following K8s controller patterns (client-go, controller-runtime). Six sub-services communicate via NATS JetStream (async) and REST (sync).
- **L2 Agent**: Go, Pion WebRTC v4 + go-gst (GStreamer CGo). Lite ICE mode. GPU detection drives encoder selection.
- **L1 Web Client**: React 18, TypeScript, Vite, Zustand (state), Ant Design (UI). WebRTC API for media, WebSocket for signaling.
- **L1 Tauri Client**: Rust backend + TypeScript/Vue/React frontend. Shares same Broker API as Web client, distinguished by `clientType` field.
- **GStreamer**: Must run as separate process for LGPL isolation. Dynamic linking only.
- **K8s Operators**: Use kubebuilder or operator-sdk for KubeVirt CRDs, Longhorn volume management, Kube-OVN network policies.