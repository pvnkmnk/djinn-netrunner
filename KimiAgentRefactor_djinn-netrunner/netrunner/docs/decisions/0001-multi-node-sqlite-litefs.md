# ADR 0001: Multi-node SQLite Coordination with LiteFS

## Status
Proposed

## Context
Djinn NETRUNNER is designed as a standalone "streaming appliance." While current deployments are single-node, there is a requirement to support multi-node environments (e.g., separate worker nodes for downloading vs. UI nodes for streaming) without abandoning the simplicity and portability of SQLite.

We evaluated several options for distributed SQLite:
1. **LiteFS**: Page-level replication via FUSE, single-primary write model.
2. **rqlite**: Raft-based consensus, HTTP API interface.
3. **Shared Mount (NFS)**: Standard SQLite with advisory locking.

## Decision
We will adopt **LiteFS** as the production-grade data layer for multi-node Djinn NETRUNNER deployments.

### Rationale
- **Portability**: LiteFS keeps the database as a standard SQLite file, preserving the "appliance" vision.
- **Performance**: Reads are local and extremely fast on every node.
- **Simplicity**: Does not require replacing the GORM/SQLite driver with an HTTP client (unlike rqlite).
- **Ecosystem**: LiteFS is well-supported in containerized environments (Docker/Kubernetes).

## Implementation Strategy
1. **Primary Node**: One node will be designated as the primary. All writes (Job enqueuing, settings updates) must occur on this node.
2. **Replica Nodes**: All other nodes will be read-only replicas.
3. **Write Forwarding**: Replicas will use an HTTP middleware to detect the primary node (via the `.primary` file) and forward write requests (POST/PUT/DELETE) to it.
4. **Worker Coordination**: 
   - By default, the `WorkerOrchestrator` will only run on the Primary node.
   - If horizontal worker scaling is needed, replicas can run workers but must proxy "Job Claim" and "Job Completion" updates to the primary.

## Consequences
- **FUSE Dependency**: LiteFS requires FUSE, which may not be available on all host operating systems (best run in Docker).
- **Write Latency**: Writes on replica nodes will incur network latency as they are forwarded to the primary.
- **Failover**: LiteFS handles leader election, but the application must be resilient to brief "read-only" periods during elections.
