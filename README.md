*This project has been created as part of the 42 curriculum by viceda-s.*

# ft_lgtm

Build a web application that runs untrusted code securely with WASM/WASI, uploads it to IPFS, and monitors everything with a complete LGTM observability stack on Kubernetes.

## Description

LGTM (Looks Good To Monitor) is a web application that lets users write and execute code snippets safely, then observe every step of that execution in detail. Submitted code is compiled to WebAssembly and run inside a sandboxed WASI runtime, its source and output are packaged and uploaded to IPFS for sharing, and every stage — compilation, execution, upload — is instrumented with OpenTelemetry and visualized through a full LGTM observability stack (Grafana, Loki, Tempo, Mimir) running on a local Kubernetes cluster.

The goal is to demonstrate, end to end, how modern applications combine sandboxed code execution, distributed storage, and observability so that nothing about a running system has to be guessed at.

## Instructions

The entire project runs inside a local Kubernetes cluster (k3d) provisioned by a single `Makefile`.

```bash
make up      # installs host dependencies, creates the k3d cluster, deploys ingress-nginx and Kubo (IPFS)
make down    # tears down the k3d cluster
make status  # shows cluster/pod status
```

Once `make up` completes, the application and its services are reachable at:

- `lgtm.local` — the web application
- `grafana.lgtm.local` — Grafana dashboards
- `ipfs.lgtm.local` — the IPFS gateway

These hostnames must resolve to `127.0.0.1`, e.g. via `/etc/hosts`:

```bash
echo "127.0.0.1 lgtm.local grafana.lgtm.local ipfs.lgtm.local" | sudo tee -a /etc/hosts
```

## Resources

- [WebAssembly](https://webassembly.org/) and [WASI](https://wasi.dev/)
- [Kubo (IPFS implementation)](https://github.com/ipfs/kubo) and the [IPFS documentation](https://docs.ipfs.tech/)
- [OpenTelemetry documentation](https://opentelemetry.io/docs/)
- [Grafana LGTM stack](https://grafana.com/oss/) (Grafana, Loki, Tempo, Mimir)
- [k3d documentation](https://k3d.io/)

**AI usage:** AI assistance (Claude) was used to help with the project structure and with debugging code during development.

## IPFS public reachability

This project runs entirely inside a local VM, as required by the subject. IPFS is a peer-to-peer network, so public reachability depends on the underlying network configuration.

- Inside the cluster/VM, the gateway is always available at: `http://ipfs.lgtm.local/ipfs/<CID>/main.go`
- Public reachability (e.g. fetching the same CID via `https://ipfs.io/ipfs/<CID>/main.go`) requires the node to be reachable from the public internet. The subject explicitly allows explaining NAT limitations to evaluators:
  > "If you are behind NAT without UPnP / IPv6, explain the limitations to your evaluators." [subject, V.1.4]

Where router port-forwarding or a public/IPv6 address is available, the same CID is tested from another network. Note that forwarding the swarm port (4001) alone is a best-effort attempt, not a guarantee — Kubo may still announce internal cluster/pod addresses that a remote peer can't route to, depending on the network setup. Where reachability isn't achieved, this limitation is documented here and demonstrated locally via `ipfs.lgtm.local` instead.
