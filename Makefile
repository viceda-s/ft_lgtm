SHELL := /bin/bash
K3D_CLUSTER_NAME := lgtm
K3D_CONFIG := infra/k3d/cluster-config.yaml

GREEN  := \033[0;32m
BLUE   := \033[0;34m
RED    := \033[0;31m
BOLD   := \033[1m
RESET  := \033[0m

# Runs the shell function named $1 (with args $2) in the background with a
# spinner labeled $1, logging output to a temp file only shown on failure.
# The command is invoked as a shell function (not a $(call) argument) so
# Make's text substitution never touches embedded quotes/brackets.
define run_spinner
	label="$(1)"; log="$$(mktemp)"; \
	( $(2) ) >"$$log" 2>&1 & pid=$$!; \
	spin='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'; i=0; \
	while kill -0 $$pid 2>/dev/null; do \
		i=$$(( (i+1) % 10 )); \
		printf "\r$(BLUE)%s$(RESET) %s" "$${spin:$$i:1}" "$$label"; \
		sleep 0.1; \
	done; \
	wait $$pid; status=$$?; \
	if [ $$status -eq 0 ]; then \
		printf "\r$(GREEN)✓$(RESET) %s\n" "$$label"; \
	else \
		printf "\r$(RED)✗$(RESET) %s\n" "$$label"; \
		cat "$$log"; \
	fi; \
	rm -f "$$log"; \
	exit $$status
endef

.PHONY: install up down status

install:
	@printf "$(BOLD)>> Checking/installing host dependencies (Docker, k3d, kubectl, helm)$(RESET)\n"
	@if ! command -v docker >/dev/null 2>&1; then \
		$(call run_spinner,Installing Docker...,\
			sudo apt-get update && \
			sudo apt-get install -y ca-certificates curl gnupg && \
			sudo install -m 0755 -d /etc/apt/keyrings && \
			curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg && \
			sudo chmod a+r /etc/apt/keyrings/docker.gpg && \
			echo "deb [arch=$$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $$(. /etc/os-release && echo $$VERSION_CODENAME) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null && \
			sudo apt-get update && \
			sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin && \
			sudo usermod -aG docker $$USER); \
		echo "  (you may need to log out/in for docker group membership to apply)"; \
	else \
		printf "$(GREEN)✓$(RESET) Docker already installed: %s\n" "$$(docker --version)"; \
	fi
	@if ! command -v kubectl >/dev/null 2>&1; then \
		$(call run_spinner,Installing kubectl...,\
			curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/$$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
			sudo install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
			rm -f /tmp/kubectl); \
	else \
		printf "$(GREEN)✓$(RESET) kubectl already installed: %s\n" "$$(kubectl version --client --short 2>/dev/null || kubectl version --client)"; \
	fi
	@if ! command -v k3d >/dev/null 2>&1; then \
		$(call run_spinner,Installing k3d...,\
			curl -fsSL https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash); \
	else \
		printf "$(GREEN)✓$(RESET) k3d already installed: %s\n" "$$(k3d version | head -1)"; \
	fi
	@if ! command -v helm >/dev/null 2>&1; then \
		$(call run_spinner,Installing helm...,\
			curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash); \
	else \
		printf "$(GREEN)✓$(RESET) helm already installed: %s\n" "$$(helm version --short)"; \
	fi
	@printf "$(BOLD)>> All host dependencies present.$(RESET)\n"

up: install
	@if k3d cluster list | grep -q "^$(K3D_CLUSTER_NAME) "; then \
		printf "$(GREEN)✓$(RESET) Cluster '$(K3D_CLUSTER_NAME)' already exists, skipping creation.\n"; \
	else \
		$(call run_spinner,Creating k3d cluster '$(K3D_CLUSTER_NAME)'...,\
			k3d cluster create --config $(K3D_CONFIG)); \
	fi
	kubectl config use-context k3d-$(K3D_CLUSTER_NAME)
	@printf "$(BOLD)>> Cluster is up. Nodes:$(RESET)\n"
	kubectl get nodes

down:
	@printf "$(BOLD)>> Deleting k3d cluster '$(K3D_CLUSTER_NAME)'$(RESET)\n"
	k3d cluster delete $(K3D_CLUSTER_NAME)

status:
	@k3d cluster list
	@kubectl get nodes 2>/dev/null || true