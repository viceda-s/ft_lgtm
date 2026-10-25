_This project has been created as part of the 42 curriculum by luis-fif, viceda-s._

# ft_lgtm

Build a web application that runs untrusted code securely with WASM/WASI, uploads it to IPFS, and monitors everything with a complete LGTM observability stack on Kubernetes.

## Description

LGTM (Looks Good To Monitor) is a web application that lets users write and execute code snippets safely, then observe every step of that execution in detail. Submitted code is compiled to WebAssembly and run inside a sandboxed WASI runtime, its source and output are packaged and uploaded to IPFS for sharing, and every stage — compilation, execution, upload — is instrumented with OpenTelemetry and visualized through a full LGTM observability stack (Grafana, Loki, Tempo, Mimir) running on a local Kubernetes cluster.

The goal is to demonstrate, end to end, how modern applications combine sandboxed code execution, distributed storage, and observability so that nothing about a running system has to be guessed at.

## Instructions

The entire project runs inside a local Kubernetes cluster (k3d) provisioned by a single `Makefile`.

```bash
make         # same as make up
make up      # installs host dependencies, maps the hostnames below in /etc/hosts, creates the k3d cluster and deploys ingress-nginx, Kubo (IPFS), the LGTM stack, the backend and the frontend
make down    # tears down the k3d cluster
make status  # shows cluster/pod status
```

Once `make up` completes, the application and its services are reachable at:

- `lgtm.local` — the web application
- `grafana.lgtm.local` — Grafana dashboards (login `admin` / `admin`)
- `ipfs.lgtm.local` — the IPFS gateway

`make up` points these hostnames at `127.0.0.1` in `/etc/hosts` when they are missing.

## Resources

- [WebAssembly](https://webassembly.org/) and [WASI](https://wasi.dev/)
- [Kubo (IPFS implementation)](https://github.com/ipfs/kubo) and the [IPFS documentation](https://docs.ipfs.tech/)
- [OpenTelemetry documentation](https://opentelemetry.io/docs/)
- [Grafana LGTM stack](https://grafana.com/oss/) (Grafana, Loki, Tempo, Mimir)
- [k3d documentation](https://k3d.io/)

**AI usage:** AI assistance (Claude) was used to help with the project structure and with debugging code during development. Claude Code was also used for a review of the whole repository, which led to these changes:

- security: rejecting cgo at compile time (a C `#include` could print files from the pod) and hardening the backend pod;
- observability: execution counters that start at zero so Dashboard 1 counts the first runs, root-span queries and a slowest-executions table in Dashboard 2, and the trace-to-logs query;
- playground: the example program and the display of stage timings, the trace link and truncated output;
- infrastructure: Makefile fixes for fresh VMs, the backend image layer order, Kubo and Helm values fixes, and the tests covering these changes.

## IPFS public reachability

This project runs entirely inside a local VM, as required by the subject. IPFS is a peer-to-peer network, so public reachability depends on the underlying network configuration.

- Inside the cluster/VM, the gateway is always available at: `http://ipfs.lgtm.local/ipfs/<CID>/main.go`
- Public reachability (e.g. fetching the same CID via `https://ipfs.io/ipfs/<CID>/main.go`) requires the node to be reachable from the public internet. The subject explicitly allows explaining NAT limitations to evaluators:
  > "If you are behind NAT without UPnP / IPv6, explain the limitations to your evaluators." [subject, V.1.4]

The k3d load balancer publishes Kubo's swarm port (4001, TCP and UDP) on the VM; the RPC API (5001) is never exposed. Where router port-forwarding or a public/IPv6 address is available, the same CID is tested from another network. Note that forwarding the swarm port (4001) alone is a best-effort attempt, not a guarantee — Kubo may still announce internal cluster/pod addresses that a remote peer can't route to, depending on the network setup. Where reachability isn't achieved, this limitation is documented here and demonstrated locally via `ipfs.lgtm.local` instead.

## Bonus features

- **Social sharing:** every run that reaches IPFS gets a short link such as `http://lgtm.local/s/k4twd4jo`. Opening it loads the code into the editor. The id is derived from the CID, so the same code and output always give the same link, and it is stored as an MFS entry (`/shares/<id>`) on Kubo's persistent volume, so no extra database is needed. `GET /api/share/<id>` returns the code as JSON, and creating a link shows up as a `share_link` span in the execution trace.
