# Artifact Storage at Scale

This document outlines the architecture, challenges, and proposed designs for high-throughput, large-scale artifact management in Kestrix.

---

## 1. Current State: In-Database & Host Artifacts

Kestrix supports artifact passing between DAG tasks across local, Docker, and remote runners. In the current implementation:

1. **Artifact Declaration:** A task specifies `artifacts = ["path/to/output", ...]` (max 32 entries).
2. **Transfer:**
   - Remote runner creates a `.tar.gz` archive of declared paths upon task exit code 0.
   - Runner uploads archive via `PUT /api/runner-jobs/{id}/artifacts?epoch=N`.
   - Payload is stored in PostgreSQL (`remote_artifacts` table) with size limit `KESTRIX_RUNNER_ARTIFACT_MAX_BYTES` (default 50 MiB).
3. **Dependency Injection:** When dependent tasks are dispatched, Kestrix retrieves stored artifacts for all passed ancestor tasks in topological order and attaches them to the new job payload.
4. **Lifecycle & Cleanup:** Artifacts are deleted automatically when the parent run reaches a terminal state (`passed`, `failed`, `canceled`).

---

## 2. Scalability Challenges with Large Binaries

While suitable for lightweight artifacts (test reports, small binaries, manifests, config files), workloads at scale encounter real constraints:

### A. Database Bloat & Write Amplification
- Storing 50 MiB–500 MiB binaries directly in PostgreSQL bytea/blob columns rapidly bloats the database table and Write-Ahead Log (WAL).
- High database disk I/O degrades performance for core scheduling and run status updates.
- Backups (pg_dump/snapshots) become slow and oversized due to transient build archives.

### B. Control-Plane Bandwidth Saturation
- Every byte of artifact data travels through the Kestrix server:
  `Runner A (Upload) -> Kestrix Host -> PostgreSQL -> Kestrix Host -> Runner B (Download)`
- In a pipeline with parallel branches, a 200 MiB artifact duplicated across 5 downstream tasks results in 1 GiB of network egress on the central Kestrix instance.

### C. No Cross-Run Deduplication or Layer Caching
- Artifacts are tied strictly to a single `run_id`.
- Re-running a pipeline or building similar commits re-uploads identical dependencies and vendor directories, wasting storage and network transit.

---

## 3. Target Architecture: Pluggable Object Storage with Direct Presigned URLs

To scale to gigabyte-sized artifacts and hundreds of concurrent jobs, Kestrix should decouple artifact data storage from the relational control plane.

```
                  ┌───────────────────────────────┐
                  │         Kestrix API           │
                  │   (Control Plane Metadata)    │
                  └──────┬─────────────────▲──────┘
         1. Request      │                 │ 4. Notify Upload
            Upload URL   │                 │    Complete
                         ▼                 │
                  ┌──────────────┐         │
                  │ Runner A     │─────────┘
                  │ (Producer)   │
                  └──────┬───────┘
                         │ 2. Direct S3 PUT (presigned)
                         ▼
        ┌──────────────────────────────────────────────────┐
        │       Object Storage (S3 / MinIO / GCS)          │
        │   s3://kestrix-artifacts/{run_id}/{task_id}.tar  │
        └────────────────────────▲─────────────────────────┘
                                 │
                                 │ 3. Direct S3 GET (presigned)
                                 │
                          ┌──────┴───────┐
                          │ Runner B     │
                          │ (Consumer)   │
                          └──────────────┘
```

### Key Components:

### 1. `domain.ArtifactStore` Interface
Abstract storage behind a clean port interface:
```go
type ArtifactStore interface {
    // Put stores artifact data and returns storage URI
    Put(ctx context.Context, key string, r io.Reader, size int64) (string, error)
    // Get returns reader for the stored artifact
    Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
    // GenerateUploadURL creates a presigned PUT URL for direct runner upload
    GenerateUploadURL(ctx context.Context, key string, maxBytes int64, ttl time.Duration) (string, error)
    // GenerateDownloadURL creates a presigned GET URL for direct runner download
    GenerateDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error)
    // DeleteByRun removes all artifacts associated with a run
    DeleteByRun(ctx context.Context, runID int) error
}
```

Implementations:
- `PostgresArtifactStore`: current default for zero-dependency standalone deployments.
- `S3ArtifactStore`: AWS S3, MinIO, Cloudflare R2, Google Cloud Storage, Ceph.
- `LocalDiskArtifactStore`: shared NFS / host volume mount.

### 2. Direct-to-Storage Transfer via Presigned URLs
- When a task succeeds, runner asks Kestrix for an upload target: `POST /api/runner-jobs/{id}/artifacts/upload-url`.
- Kestrix generates a presigned S3 PUT URL with short expiration (e.g., 15 minutes) and strict content length restrictions.
- Runner streams archive directly to S3/MinIO. Central server network traffic = 0 bytes.
- When Runner B claims dependent job, job payload includes direct presigned GET URLs to download artifacts from storage.

### 3. Content-Addressable Storage (CAS) Deduplication
- Compute SHA-256 checksum of artifact tarballs before upload.
- If storage already has an object with `cas/<sha256>`, mark reference in database and skip re-uploading identical bytes.
- Significant bandwidth savings for static build dependencies, node_modules, or compiled vendor caches.

### 4. Retention & Lifecycle Policies
- **Ephemeral Run Artifacts:** Retained during active run + configurable grace period (e.g., 24 hours), then pruned by background reaper or S3 bucket lifecycle rules.
- **Exported Pipeline Artifacts:** Tagged for long-term retention (release binaries, compliance reports) with configurable retention days (`expire_in = "30d"`).

---

## 4. Migration & Compatibility

1. **Zero Configuration Default:** Deployments without S3 credentials continue using Postgres storage transparently.
2. **Environment Variables:**
   ```bash
   KESTRIX_ARTIFACT_STORE=s3                 # postgres (default), s3, disk
   KESTRIX_S3_ENDPOINT=minio:9000
   KESTRIX_S3_BUCKET=kestrix-artifacts
   KESTRIX_S3_REGION=us-east-1
   KESTRIX_S3_ACCESS_KEY=...
   KESTRIX_S3_SECRET_KEY=...
   KESTRIX_S3_USE_SSL=true
   KESTRIX_ARTIFACT_MAX_BYTES=2147483648     # 2 GiB
   ```
3. **Graceful Fallback:** If runner cannot reach direct S3 endpoint (network segregation), fallback proxy route through Kestrix API remains available.
