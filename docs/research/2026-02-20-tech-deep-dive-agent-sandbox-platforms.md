---
date: 2026-02-20
topic: agent-sandbox-platforms
mode: tech-deep-dive
status: complete
tags: [sandboxes, ai-agents, infrastructure]
confidence: medium
---

# Agent Sandbox Platforms: Modal vs Sprites vs Vercel vs Cloudflare vs Rivet and Others

## Summary

For agentic code execution, the market splits into two product categories: (1) managed execution substrates (microVM/container sandbox providers) and (2) control-plane tooling that runs inside those substrates. Modal, Fly Sprites, Vercel Sandbox, Cloudflare Sandbox SDK/Containers, E2B, Daytona, and CodeSandbox SDK are direct execution substrates; Rivet's `sandboxagent.dev` is best interpreted as control-plane middleware that runs inside another substrate.

If your top requirement is running large volumes of untrusted agent code with snapshotting and runtime-defined environments, the strongest direct fits are Modal, Vercel Sandbox, Fly Sprites, E2B, and Daytona, with Cloudflare Sandbox best when you are already committed to Workers and edge-native architecture.

## Research Question

What are the practical differences between major agent sandbox products (Modal, Fly Sprites, Vercel Sandbox, Cloudflare Sandbox/Containers, Rivet/Sandbox Agent, and other notable options), especially around isolation, latency/startup, persistence, networking, limits, and pricing?

## Key Findings

### The category is split between infrastructure and orchestration

`sandboxagent.dev` describes itself as "a server that runs inside your sandbox" and explicitly lists E2B/Daytona/Vercel as execution targets, which positions it as an agent control API layer, not a standalone sandbox compute substrate ([sandboxagent.dev](https://sandboxagent.dev/), [docs](https://sandboxagent.dev/docs)).

### Isolation model differs materially by vendor

- Vercel explicitly uses Firecracker microVMs for each sandbox ([concepts](https://vercel.com/docs/vercel-sandbox/concepts)).
- Fly Sprites explicitly markets Firecracker VMs and hardware-isolated execution ([sprites.dev](https://sprites.dev), [overview](https://docs.sprites.dev/)).
- Modal sandboxes are built on gVisor and isolate at hardened container runtime boundaries ([networking/security](https://modal.com/docs/guide/sandbox-networking), [security](https://modal.com/docs/guide/security)).
- Cloudflare Containers run each instance in its own VM, while Cloudflare Sandbox SDK is built on Containers ([architecture](https://developers.cloudflare.com/containers/platform-details/architecture/), [sandbox index](https://developers.cloudflare.com/sandbox/)).
- Azure dynamic sessions use Hyper-V sandboxing ([Azure sessions](https://learn.microsoft.com/en-us/azure/container-apps/sessions)).

### Persistence and resume behavior are major differentiators

- Modal supports filesystem snapshots (indefinite), directory snapshots (beta), and memory snapshots (alpha with expiration/limitations) ([Modal snapshots](https://modal.com/docs/guide/sandbox-snapshots)).
- Fly Sprites emphasizes persistent ext4 filesystem + checkpoint/restore ([sprites.dev](https://sprites.dev), [docs overview](https://docs.sprites.dev/)).
- Vercel supports snapshots and documents snapshot storage + expiration controls ([pricing/limits](https://vercel.com/docs/vercel-sandbox/pricing), [concepts](https://vercel.com/docs/vercel-sandbox/concepts)).
- E2B supports pause/resume persistence including memory + filesystem state (beta) ([E2B persistence](https://e2b.dev/docs/sandbox/persistence)).
- Cloudflare Containers currently document ephemeral disk on sleep/restart ([containers FAQ](https://developers.cloudflare.com/containers/faq/), [architecture](https://developers.cloudflare.com/containers/platform-details/architecture/)).

### Runtime ceilings and region stories vary a lot

- Vercel Sandbox is currently `iad1` only, with plan-specific duration/concurrency limits ([Vercel pricing/limits](https://vercel.com/docs/vercel-sandbox/pricing)).
- Modal sandbox timeout defaults to 5 minutes and can be set up to 24 hours per sandbox ([Modal sandboxes](https://modal.com/docs/guide/sandboxes)).
- E2B documents base/pro runtime ceilings and timeout semantics ([E2B lifecycle](https://e2b.dev/docs/sandbox)).
- Cloudflare positions global placement via Workers/DO + pre-fetched images and often 2-3s cold starts for containers ([architecture](https://developers.cloudflare.com/containers/platform-details/architecture/), [FAQ](https://developers.cloudflare.com/containers/faq/)).
- Azure dynamic sessions advertises millisecond allocation with pooled sessions and broad regional coverage ([Azure sessions](https://learn.microsoft.com/en-us/azure/container-apps/sessions)).

### Pricing dimensions are converging

Most platforms now meter a mix of CPU-time, memory-time, storage/snapshots, and network egress, often with short billing quanta and scale-to-zero behavior (Modal, Sprites, Vercel, Cloudflare, E2B, Daytona, CodeSandbox SDK) ([Modal pricing](https://modal.com/pricing), [sprites.dev billing](https://sprites.dev), [Vercel pricing/limits](https://vercel.com/docs/vercel-sandbox/pricing), [Cloudflare containers pricing](https://developers.cloudflare.com/containers/pricing/), [E2B pricing](https://e2b.dev/pricing), [Daytona pricing](https://www.daytona.io/pricing), [CodeSandbox pricing](https://codesandbox.io/pricing)).

## Comparison Matrix

| Criterion | Modal Sandboxes | Fly Sprites | Vercel Sandbox | Cloudflare Sandbox SDK / Containers | Rivet Sandbox Agent (`sandboxagent.dev`) | E2B | Daytona | CodeSandbox SDK |
|---|---|---|---|---|---|---|---|---|
| Execution primitive | Runtime-defined secure containers ([Modal sandboxes](https://modal.com/docs/guide/sandboxes)) | Firecracker VMs + persistent env ([sprites.dev](https://sprites.dev)) | Firecracker microVMs ([concepts](https://vercel.com/docs/vercel-sandbox/concepts)) | Sandbox SDK on top of Containers; Containers run in VMs ([sandbox](https://developers.cloudflare.com/sandbox/), [architecture](https://developers.cloudflare.com/containers/platform-details/architecture/)) | Control server inside someone else's sandbox ([home](https://sandboxagent.dev/), [docs](https://sandboxagent.dev/docs)) | Linux VM sandboxes ([E2B docs](https://e2b.dev/docs)) | Isolated sandboxes via Daytona SDK ([docs](https://www.daytona.io/docs/en)) | VM-based sandboxes via SDK ([SDK intro](https://codesandbox.io/docs/sdk), [VM overview](https://codesandbox.io/docs/learn/vm-sandboxes/overview)) |
| Isolation model | gVisor-based hardened isolation ([network/security](https://modal.com/docs/guide/sandbox-networking)) | Hardware-isolated / microVM claims ([sprites.dev](https://sprites.dev), [docs](https://docs.sprites.dev/)) | Dedicated microVM per sandbox ([concepts](https://vercel.com/docs/vercel-sandbox/concepts)) | Container instance in its own VM; Workers+DO control plane ([architecture](https://developers.cloudflare.com/containers/platform-details/architecture/)) | Depends on underlying provider; not an isolation substrate ([docs](https://sandboxagent.dev/docs)) | Isolated sandbox model (vendor docs) ([E2B docs](https://e2b.dev/docs)) | Isolated sandbox model (vendor docs) ([Daytona docs](https://www.daytona.io/docs/en)) | VM sandboxes and microVM snapshotting claims ([sdk](https://codesandbox.io/sdk), [vm overview](https://codesandbox.io/docs/learn/vm-sandboxes/overview)) |
| Startup / latency claims | Product page claims sub-second startup ([Modal product](https://modal.com/products/sandboxes)) | Docs/marketing emphasize fast wake/cold request often <1s ([sprites.dev](https://sprites.dev)) | Docs claim millisecond starts ([overview](https://vercel.com/docs/vercel-sandbox), [concepts](https://vercel.com/docs/vercel-sandbox/concepts)) | Containers cold starts often 2-3s (depends) ([FAQ](https://developers.cloudflare.com/containers/faq/)) | N/A (middleware) | Fast startup positioning in docs ([E2B docs](https://e2b.dev/docs)) | Pricing page claims millisecond spin-up ([Daytona pricing](https://www.daytona.io/pricing)) | SDK/site claims fast spin-up and ~2s clone ([sdk](https://codesandbox.io/sdk), [vm overview](https://codesandbox.io/docs/learn/vm-sandboxes/overview)) |
| Runtime/session limits | Default 5m; configurable up to 24h per sandbox ([sandboxes](https://modal.com/docs/guide/sandboxes)) | Persistent env, wake/sleep model; no single fixed max highlighted on homepage/docs captured | Default 5m, plan max up to 5h; concurrency limits by plan ([pricing](https://vercel.com/docs/vercel-sandbox/pricing)) | No fixed hard max in FAQ; lifecycle tied to sleep/restarts/host events ([FAQ](https://developers.cloudflare.com/containers/faq/)) | N/A | Default 5m timeout; tier limits for continuous runtime ([sandbox](https://e2b.dev/docs/sandbox)) | Tier/resource limits in org + docs pages ([limits/billing docs](https://www.daytona.io/docs/en/limits)) | Plan-based concurrency/rate limits and unlimited session length in pricing table ([pricing](https://codesandbox.io/pricing)) |
| Persistence / snapshots | Filesystem, directory beta, memory alpha snapshots ([snapshots](https://modal.com/docs/guide/sandbox-snapshots)) | Persistent ext4 + checkpoints ([sprites.dev](https://sprites.dev), [overview](https://docs.sprites.dev/)) | Snapshot support + storage billing/expiration ([pricing](https://vercel.com/docs/vercel-sandbox/pricing), [concepts](https://vercel.com/docs/vercel-sandbox/concepts)) | Disk is currently ephemeral for Containers; Sandbox inherits platform characteristics ([FAQ](https://developers.cloudflare.com/containers/faq/), [sandbox limits](https://developers.cloudflare.com/sandbox/platform/limits/)) | Session persistence abstraction/API for agent transcripts/events ([docs](https://sandboxagent.dev/docs)) | Pause/resume includes memory+filesystem (beta) ([persistence](https://e2b.dev/docs/sandbox/persistence)) | Snapshot-based template workflow; lifecycle controls ([snapshots](https://www.daytona.io/docs/en/snapshots)) | Hibernate/resume and snapshot-oriented model in SDK docs/site ([sdk](https://codesandbox.io/sdk), [docs](https://codesandbox.io/docs/sdk)) |
| Networking model | Block-all/cidr allowlist, tunnels/connect tokens ([network/security](https://modal.com/docs/guide/sandbox-networking)) | URL routing on port 8080, outbound policies ([sprites.dev](https://sprites.dev)) | Outbound access by default + configurable firewall concepts ([concepts](https://vercel.com/docs/vercel-sandbox/concepts)) | Worker/DO mediated routing; no direct inbound TCP/UDP from end users; optional internet toggles ([architecture](https://developers.cloudflare.com/containers/platform-details/architecture/), [FAQ](https://developers.cloudflare.com/containers/faq/)) | Exposes HTTP/SSE API; security token optional depending deployment ([docs](https://sandboxagent.dev/docs)) | Network controls documented in SDK/docs | Tier + allowlist/block-all controls ([network limits](https://www.daytona.io/docs/en/network-limits)) | Hosted VM networking + preview URLs/docs |
| Runtime/language support | Arbitrary containerized commands (polyglot) ([sandboxes](https://modal.com/docs/guide/sandboxes)) | Full Linux env (polyglot) ([docs](https://docs.sprites.dev/)) | Node and Python runtime images with package managers ([system specs](https://vercel.com/docs/vercel-sandbox/system-specifications)) | Full Linux containers; language-agnostic via image ([containers](https://developers.cloudflare.com/containers/)) | Agent-control layer for Claude/Codex/OpenCode/Amp/Pi ([home](https://sandboxagent.dev/)) | Python/JS SDKs and templates ([E2B docs](https://e2b.dev/docs)) | Multi-language SDKs + snapshot images ([Daytona docs](https://www.daytona.io/docs/en)) | SDK for programmatic VM environments; language-agnostic in VM |
| Regions/global | Region selection with pricing multipliers ([region selection](https://modal.com/docs/guide/region-selection)) | Multi-region positioning via Fly network/docs; details vary by account | Currently `iad1` only ([pricing](https://vercel.com/docs/vercel-sandbox/pricing)) | Global edge story through Workers/DO placement ([architecture](https://developers.cloudflare.com/containers/platform-details/architecture/)) | Depends on chosen provider | Region support depends on E2B infra/tier/docs | Regional controls in docs ([regions](https://www.daytona.io/docs/en/regions)) | US/APAC/EMEA note on SDK landing page ([sdk](https://codesandbox.io/sdk)) |
| Pricing model | CPU+memory (sandbox rates differ), usage-based ([Modal pricing](https://modal.com/pricing)) | CPU-time, memory-time, storage/hot storage usage ([sprites.dev](https://sprites.dev)) | Active CPU, provisioned memory, creations, network, snapshot storage ([pricing](https://vercel.com/docs/vercel-sandbox/pricing)) | Containers pricing (CPU/memory/disk/egress) + Workers + DO + logs ([containers pricing](https://developers.cloudflare.com/containers/pricing/), [sandbox pricing](https://developers.cloudflare.com/sandbox/platform/pricing/)) | OSS/sdk layer pricing separate from compute provider | Subscription + usage (CPU/RAM/storage, concurrency/session caps) ([E2B pricing](https://e2b.dev/pricing)) | Usage-based (vCPU, RAM, storage) + credits ([Daytona pricing](https://www.daytona.io/pricing)) | Subscription + VM credits/usage + concurrency limits ([CodeSandbox pricing](https://codesandbox.io/pricing)) |

## Recommendation

For agent sandbox selection, use this decision flow:

1. If you need a **provider-neutral agent control layer**, use `sandboxagent.dev` *on top of* a compute sandbox provider (not instead of one) ([docs](https://sandboxagent.dev/docs)).
2. If you need **max isolation with explicit microVM posture**, prioritize Vercel/Fly/CodeSandbox-style microVM products; validate region/runtime limits early ([Vercel concepts](https://vercel.com/docs/vercel-sandbox/concepts), [sprites.dev](https://sprites.dev)).
3. If you need **runtime-defined environments + long-lived orchestration + mature Python-first workflows**, Modal is a strong default ([Modal sandboxes](https://modal.com/docs/guide/sandboxes)).
4. If you are already on **Cloudflare Workers/Durable Objects**, Cloudflare Sandbox SDK gives architectural adjacency, but treat persistence constraints carefully while Containers disk remains ephemeral ([sandbox](https://developers.cloudflare.com/sandbox/), [containers FAQ](https://developers.cloudflare.com/containers/faq/)).
5. If your product requires **pause/resume developer-like sessions**, evaluate E2B/Daytona/CodeSandbox SDK with your exact concurrency and hibernation behavior under load ([E2B persistence](https://e2b.dev/docs/sandbox/persistence), [Daytona snapshots](https://www.daytona.io/docs/en/snapshots), [CodeSandbox SDK](https://codesandbox.io/docs/sdk)).

**Confidence:** Medium

**Caveats:**
- Several products are beta/rapidly evolving, especially Cloudflare Sandbox/Containers and parts of snapshot APIs.
- Marketing pages use different latency definitions (cold start vs resume vs clone), so run a standardized benchmark for your workload.

**Next Steps:**
1. [ ] Run a controlled bake-off on 3 finalists with identical workloads (cold start, warm resume, snapshot restore, package install, network-restricted tasks).
2. [ ] Evaluate failure modes and security posture (network controls, secret handling, blast radius) with your threat model.
3. [ ] Compare effective monthly cost using observed CPU/memory/egress traces, not list prices.

## Open Questions

- How stable are API/limits over 6-12 months for the beta-stage offerings?
- Which vendors provide enterprise controls (SOC2/HIPAA/SSO/audit) aligned with your compliance timeline?
- What is the practical p95/p99 startup for your real templates across regions and times of day?

## References

- Modal Sandboxes docs: https://modal.com/docs/guide/sandboxes
- Modal networking/security: https://modal.com/docs/guide/sandbox-networking
- Modal snapshots: https://modal.com/docs/guide/sandbox-snapshots
- Modal region selection: https://modal.com/docs/guide/region-selection
- Modal pricing: https://modal.com/pricing
- Modal product page: https://modal.com/products/sandboxes

- Fly Sprites product: https://sprites.dev
- Fly Sprites docs: https://docs.sprites.dev/
- Fly pricing/nav (Sprites listed): https://fly.io/pricing/

- Vercel Sandbox overview: https://vercel.com/docs/vercel-sandbox
- Vercel concepts: https://vercel.com/docs/vercel-sandbox/concepts
- Vercel system specs: https://vercel.com/docs/vercel-sandbox/system-specifications
- Vercel pricing/limits: https://vercel.com/docs/vercel-sandbox/pricing

- Cloudflare Sandbox SDK: https://developers.cloudflare.com/sandbox/
- Cloudflare Sandbox pricing: https://developers.cloudflare.com/sandbox/platform/pricing/
- Cloudflare Sandbox limits: https://developers.cloudflare.com/sandbox/platform/limits/
- Cloudflare Containers overview: https://developers.cloudflare.com/containers/
- Cloudflare Containers architecture/lifecycle: https://developers.cloudflare.com/containers/platform-details/architecture/
- Cloudflare Containers limits: https://developers.cloudflare.com/containers/platform-details/limits/
- Cloudflare Containers pricing: https://developers.cloudflare.com/containers/pricing/
- Cloudflare Containers FAQ: https://developers.cloudflare.com/containers/faq/

- Rivet docs overview: https://www.rivet.gg/docs
- Rivet cloud/pricing: https://www.rivet.gg/cloud
- Sandbox Agent SDK home: https://sandboxagent.dev/
- Sandbox Agent SDK docs: https://sandboxagent.dev/docs

- E2B docs index: https://e2b.dev/docs
- E2B sandbox lifecycle: https://e2b.dev/docs/sandbox
- E2B sandbox persistence: https://e2b.dev/docs/sandbox/persistence
- E2B pricing: https://e2b.dev/pricing

- Daytona docs: https://www.daytona.io/docs/en
- Daytona snapshots: https://www.daytona.io/docs/en/snapshots
- Daytona network limits: https://www.daytona.io/docs/en/network-limits
- Daytona pricing: https://www.daytona.io/pricing

- CodeSandbox SDK landing: https://codesandbox.io/sdk
- CodeSandbox SDK docs: https://codesandbox.io/docs/sdk
- CodeSandbox VM overview: https://codesandbox.io/docs/learn/vm-sandboxes/overview
- CodeSandbox pricing: https://codesandbox.io/pricing

- Azure Container Apps dynamic sessions: https://learn.microsoft.com/en-us/azure/container-apps/sessions

- AWS Lambda overview: https://docs.aws.amazon.com/lambda/latest/dg/welcome.html
- AWS Lambda SnapStart: https://docs.aws.amazon.com/lambda/latest/dg/snapstart.html

- GCP Cloud Run overview: https://cloud.google.com/run/docs/overview/what-is-cloud-run
- GCP Cloud Run pricing: https://cloud.google.com/run/pricing
