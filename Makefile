SHELL := /bin/bash
K3D_CLUSTER_NAME := lgtm
K3D_CONFIG := infra/k3d/cluster-config.yaml

GREEN  := \033[0;32m
YELLOW := \033[0;33m
BLUE   := \033[0;34m
RED    := \033[0;31m
BOLD   := \033[1m
RESET  := \033[0m
CHECK := $(BOLD)$(GREEN)✓$(RESET)
CROSS := $(BOLD)$(RED)✗$(RESET)
FIND  := $(BOLD)$(BLUE)⌕$(RESET)
DROP  := $(BOLD)$(YELLOW)⊘$(RESET)
TRASH := $(BOLD)$(YELLOW)🗑$(RESET)

# Runs the shell function named $1 (with args $2) in the background with a spinner labeled $1, logging output to a temp file only shown on failure. The command is invoked as a shell function (not a $(call) argument) so Make's text substitution never touches embedded quotes/brackets.
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
		printf "\r$(BLUE)%s$(RESET) %s\n" "$${spin:$$i:1}" "$$label"; \
	else \
		printf "\r$(CROSS) %s\n" "$$label"; \
		cat "$$log"; \
	fi; \
	rm -f "$$log"; \
	exit $$status
endef

.PHONY: install uninstall up down status smoke-test deploy-kubo deploy-lgtm-stack deploy-backend

install:
	@printf "$(FIND) $(BOLD)Checking/installing host dependencies$(RESET)\n"
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
		printf "$(CHECK) Docker installed: %s\n" "$$(docker --version)"; \
		echo "  (you may need to log out/in for docker group membership to apply)"; \
	else \
		printf "$(CHECK) Docker already installed: %s\n" "$$(docker --version)"; \
	fi
	@if ! command -v kubectl >/dev/null 2>&1; then \
		$(call run_spinner,Installing kubectl...,\
			curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/$$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
			sudo install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
			rm -f /tmp/kubectl); \
		printf "$(CHECK) kubectl installed: %s\n" "$$(kubectl version --client --short 2>/dev/null || kubectl version --client)"; \
	else \
		printf "$(CHECK) kubectl already installed: %s\n" "$$(kubectl version --client --short 2>/dev/null || kubectl version --client)"; \
	fi
	@if ! command -v k3d >/dev/null 2>&1; then \
		$(call run_spinner,Installing k3d...,\
			curl -fsSL https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash); \
		printf "$(CHECK) k3d installed: %s\n" "$$(k3d version | head -1)"; \
	else \
		printf "$(CHECK) k3d already installed: %s\n" "$$(k3d version | head -1)"; \
	fi
	@if ! command -v helm >/dev/null 2>&1; then \
		$(call run_spinner,Installing helm...,\
			curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash); \
		printf "$(CHECK) helm installed: %s\n" "$$(helm version --short)"; \
	else \
		printf "$(CHECK) helm already installed: %s\n" "$$(helm version --short)"; \
	fi
	@$(call run_spinner,Adding Helm repos...,\
		helm repo add grafana https://grafana.github.io/helm-charts --force-update && \
		helm repo add grafana-community https://grafana-community.github.io/helm-charts --force-update && \
		helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update && \
		helm repo update)
	@printf "$(CHECK) $(BOLD)Helm repos added.$(RESET)\n"
	@echo
	@if ! command -v go >/dev/null 2>&1; then \
		$(call run_spinner,Installing Go...,\
			curl -fsSL -o /tmp/go.tar.gz https://go.dev/dl/go1.27.0.linux-amd64.tar.gz && \
			sudo rm -rf /usr/local/go && \
			sudo tar -C /usr/local -xzf /tmp/go.tar.gz && \
			sudo ln -sf /usr/local/go/bin/go /usr/local/bin/go && \
			sudo ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt && \
			rm -f /tmp/go.tar.gz); \
		printf "$(CHECK) Go installed: %s\n" "$$(go version)"; \
	else \
		printf "$(CHECK) Go already installed: %s\n" "$$(go version)"; \
	fi

	@printf "$(CHECK) $(BOLD)All host dependencies present.$(RESET)\n"
	@echo

uninstall:
	@printf "$(DROP) $(BOLD)Removing host dependencies$(RESET)\n"
	@$(call run_spinner,Removing k3d...,\
		sudo rm -f $$(command -v k3d))
	@printf "$(CHECK) k3d removed.\n"
	@$(call run_spinner,Removing helm...,\
		sudo rm -f $$(command -v helm))
	@printf "$(CHECK) helm removed.\n"
	@$(call run_spinner,Removing kubectl...,\
		sudo rm -f $$(command -v kubectl))
	@printf "$(CHECK) kubectl removed.\n"
	@$(call run_spinner,Removing Docker...,\
		sudo apt-get purge -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin && \
		sudo rm -rf /var/lib/docker /var/lib/containerd && \
		sudo rm -f /etc/apt/sources.list.d/docker.list /etc/apt/keyrings/docker.gpg)
	@printf "$(CHECK) Docker removed.\n"
	@$(call run_spinner,Removing Go...,\
		sudo rm -rf /usr/local/go /usr/local/bin/go /usr/local/bin/gofmt)
	@printf "$(CHECK) Go removed.\n"
	@printf "$(CHECK) $(BOLD)Host dependencies removed.$(RESET)\n"
	@echo


up: install
	@if k3d cluster list | grep -q "^$(K3D_CLUSTER_NAME) "; then \
		printf "$(CHECK) Cluster '$(K3D_CLUSTER_NAME)' already exists, skipping creation.\n"; \
	else \
		$(call run_spinner,Creating k3d cluster '$(K3D_CLUSTER_NAME)'...,\
			k3d cluster create --config $(K3D_CONFIG)); \
		printf "$(CHECK) Cluster '$(K3D_CLUSTER_NAME)' created.\n"; \
	fi
	@kubectl config use-context k3d-$(K3D_CLUSTER_NAME) >/dev/null
	@printf "$(CHECK) Context switched to k3d-$(K3D_CLUSTER_NAME).\n"
	@$(call run_spinner,Waiting for nodes...,\
			kubectl wait --for=condition=ready nodes --all --timeout=60s)
	@printf "$(CHECK) $(BOLD)Cluster nodes ready.$(RESET)\n"
	@echo

	@$(call run_spinner,Installing ingress-nginx...,\
			helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx --force-update && \
			helm repo update && \
			helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
				 --namespace ingress-nginx --create-namespace \
				 --set controller.service.type=LoadBalancer \
				 --wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Ingress-nginx installed and ready.$(RESET)\n"
	@echo

	@$(MAKE) --no-print-directory deploy-kubo
	@$(MAKE) --no-print-directory deploy-lgtm-stack
	@$(MAKE) --no-print-directory deploy-backend

deploy-kubo:
	@$(call run_spinner,Deploying Kubo (IPFS)...,\
			kubectl apply -f infra/k8s/kubo.yaml && \
			kubectl wait --for=condition=ready pod -l app=kubo --timeout=120s)
	@printf "$(CHECK) $(BOLD)Kubo (IPFS) deployed and ready.$(RESET)\n"
	@echo

deploy-lgtm-stack:
	@$(call run_spinner,Deploying Loki...,\
			helm upgrade --install loki grafana/loki \
				--namespace default \
				--values infra/helm/values-loki.yaml \
				--wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Loki deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Tempo...,\
			helm upgrade --install tempo grafana-community/tempo \
				--namespace default \
				--values infra/helm/values-tempo.yaml \
				--wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Tempo deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Mimir...,\
			helm upgrade --install mimir grafana/mimir-distributed \
				--namespace default \
				--values infra/helm/values-mimir.yaml \
				--wait --timeout 300s)
	@printf "$(CHECK) $(BOLD)Mimir deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying OpenTelemetry Collector...,\
			helm upgrade --install otel-collector open-telemetry/opentelemetry-collector \
				--namespace default \
				--values infra/helm/values-otel-collector.yaml \
				--wait --timeout 120s)
	@printf "$(CHECK) $(BOLD)OpenTelemetry Collector deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Applying Grafana datasources and dashboards...,\
			kubectl apply -n default -f infra/helm/datasources/datasources.yaml && \
			kubectl create configmap grafana-dashboard-metrics -n default \
				--from-file=infra/helm/dashboards/dashboard-metrics.json \
				--dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml | kubectl apply -n default -f - && \
			kubectl create configmap grafana-dashboard-traces -n default \
				--from-file=infra/helm/dashboards/dashboard-traces.json \
				--dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml | kubectl apply -n default -f -)
	@printf "$(CHECK) $(BOLD)Grafana datasources and dashboards applied.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Grafana...,\
			helm upgrade --install grafana grafana/grafana \
				--namespace default \
				--values infra/helm/values-grafana.yaml \
				--wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Grafana deployed and ready.$(RESET)\n"
	@echo

deploy-backend:
	@$(call run_spinner,Building backend image...,\
			docker build -t ft-lgtm-backend:dev backend/ && \
			k3d image import ft-lgtm-backend:dev -c $(K3D_CLUSTER_NAME))
	@printf "$(CHECK) $(BOLD)Backend image built and imported.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying backend...,\
			kubectl apply -f infra/k8s/backend.yaml && \
			kubectl rollout restart deployment/backend && \
			kubectl rollout status deployment/backend --timeout=60s)
	@printf "$(CHECK) $(BOLD)Backend deployed and ready.$(RESET)\n"

down:
	@$(call run_spinner,Deleting k3d cluster '$(K3D_CLUSTER_NAME)'...,\
			k3d cluster delete $(K3D_CLUSTER_NAME))
	@printf "$(TRASH) $(BOLD)Cluster '$(K3D_CLUSTER_NAME)' deleted.$(RESET)\n"
	@echo

status:
	@k3d cluster list
	@kubectl get nodes 2>/dev/null || true
	@kubectl get pods -n ingress-nginx 2>/dev/null || true

smoke-test:
	@printf "$(FIND) $(BOLD)Expecting HTTP 404 from ingress-nginx's default backend$(RESET)\n"
	curl -i 127.0.0.1
