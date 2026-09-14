SHELL := /bin/bash
K3D_CLUSTER_NAME := lgtm
K3D_CONFIG := infra/k3d/cluster-config.yaml

.PHONY: install up down status

install:
	@echo ">> Checking/installing host dependencies (Docker, k3d, kubectl, helm)"
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "Installing Docker..."; \
		sudo apt-get update; \
		sudo apt-get install -y ca-certificates curl gnupg; \
		sudo install -m 0755 -d /etc/apt/keyrings; \
		curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg; \
		sudo chmod a+r /etc/apt/keyrings/docker.gpg; \
		echo "deb [arch=$$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $$(. /etc/os-release && echo $$VERSION_CODENAME) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null; \
		sudo apt-get update; \
		sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin; \
		sudo usermod -aG docker $$USER; \
		echo "Docker installed. You may need to log out/in for group membership to apply."; \
	else \
		echo "Docker already installed: $$(docker --version)"; \
	fi
	@if ! command -v kubectl >/dev/null 2>&1; then \
		echo "Installing kubectl..."; \
		curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/$$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"; \
		sudo install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl; \
		rm -f /tmp/kubectl; \
	else \
		echo "kubectl already installed: $$(kubectl version --client --short 2>/dev/null || kubectl version --client)"; \
	fi
	@if ! command -v k3d >/dev/null 2>&1; then \
		echo "Installing k3d..."; \
		curl -fsSL https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash; \
	else \
		echo "k3d already installed: $$(k3d version)"; \
	fi
	@if ! command -v helm >/dev/null 2>&1; then \
		echo "Installing helm..."; \
		curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash; \
	else \
		echo "helm already installed: $$(helm version --short)"; \
	fi
	@echo ">> All host dependencies present."

up: install
	@if k3d cluster list | grep -q "^$(K3D_CLUSTER_NAME) "; then \
		echo ">> Cluster '$(K3D_CLUSTER_NAME)' already exists, skipping creation."; \
	else \
		echo ">> Creating k3d cluster '$(K3D_CLUSTER_NAME)'"; \
		k3d cluster create --config $(K3D_CONFIG); \
	fi
	kubectl config use-context k3d-$(K3D_CLUSTER_NAME)
	@echo ">> Cluster is up. Nodes:"
	kubectl get nodes

down:
	@echo ">> Deleting k3d cluster '$(K3D_CLUSTER_NAME)'"
	k3d cluster delete $(K3D_CLUSTER_NAME)

status:
	@k3d cluster list
	@kubectl get nodes 2>/dev/null || true