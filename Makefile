SHELL := /bin/bash
K3D_CLUSTER_NAME := lgtm
K3D_CONFIG := k8s/k3d-cluster.yaml
# pinned so a fresh VM gets the versions this project was tested with
K3D_VERSION := v5.9.0
HELM_VERSION := v3.22.1
KUBECTL_VERSION := v1.35.5
PRUNE_IMAGES = docker image prune -f --filter label=ft-lgtm >/dev/null; \
	for node in $$(docker ps --filter label=k3d.cluster=$(K3D_CLUSTER_NAME) --format '{{.Names}}' | grep -E -- '-(server|agent)-'); do \
		docker exec $$node crictl rmi --prune >/dev/null 2>&1; \
	done

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

# ends in (exit) not exit: inside an if, a bare exit would end the shell before the caller's ✓ printf
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
	(exit $$status)
endef

.PHONY: all install uninstall up down status smoke-test deploy-kubo deploy-lgtm-stack deploy-backend deploy-frontend

all: up

# sudo -v runs before each spinner: a password prompt inside the backgrounded command is hidden by the redraw
install:
	@printf "$(FIND) $(BOLD)Checking/installing host dependencies$(RESET)\n"
	@if ! command -v docker >/dev/null 2>&1; then \
		sudo -v || exit 1; \
		$(call run_spinner,Installing Docker...,\
			sudo apt-get update && \
			sudo apt-get install -y ca-certificates curl gnupg acl && \
			sudo install -m 0755 -d /etc/apt/keyrings && \
			curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg && \
			sudo chmod a+r /etc/apt/keyrings/docker.gpg && \
			echo "deb [arch=$$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $$(. /etc/os-release && echo $$VERSION_CODENAME) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null && \
			sudo apt-get update && \
			sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin && \
			sudo usermod -aG docker $$USER && \
			sudo setfacl -m user:$$USER:rw /var/run/docker.sock) && \
		printf "$(CHECK) Docker installed: %s\n" "$$(docker --version)"; \
	else \
		printf "$(CHECK) Docker already installed: %s\n" "$$(docker --version)"; \
	fi
	@if ! command -v kubectl >/dev/null 2>&1; then \
		sudo -v || exit 1; \
		$(call run_spinner,Installing kubectl...,\
			curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/$(KUBECTL_VERSION)/bin/linux/$$(dpkg --print-architecture)/kubectl" && \
			sudo install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
			rm -f /tmp/kubectl) && \
		printf "$(CHECK) kubectl installed: %s\n" "$$(kubectl version --client --short 2>/dev/null || kubectl version --client)"; \
	else \
		printf "$(CHECK) kubectl already installed: %s\n" "$$(kubectl version --client --short 2>/dev/null || kubectl version --client)"; \
	fi
	@if ! command -v k3d >/dev/null 2>&1; then \
		sudo -v || exit 1; \
		$(call run_spinner,Installing k3d...,\
			curl -fsSL https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | TAG=$(K3D_VERSION) bash) && \
		printf "$(CHECK) k3d installed: %s\n" "$$(k3d version | head -1)"; \
	else \
		printf "$(CHECK) k3d already installed: %s\n" "$$(k3d version | head -1)"; \
	fi
	@if ! command -v helm >/dev/null 2>&1; then \
		sudo -v || exit 1; \
		$(call run_spinner,Installing helm...,\
			curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | DESIRED_VERSION=$(HELM_VERSION) bash) && \
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
	@grep -q 'ipfs.lgtm.local' /etc/hosts || echo "127.0.0.1 lgtm.local grafana.lgtm.local ipfs.lgtm.local" | sudo tee -a /etc/hosts >/dev/null
	@printf "$(CHECK) lgtm.local, grafana.lgtm.local and ipfs.lgtm.local resolve to 127.0.0.1.\n"
	@printf "$(CHECK) $(BOLD)All host dependencies present.$(RESET)\n"
	@echo

uninstall:
	@printf "$(DROP) $(BOLD)Removing host dependencies$(RESET)\n"
	@sudo -v
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
	@printf "$(CHECK) $(BOLD)Host dependencies removed.$(RESET)\n"
	@echo


up: install
	@if k3d cluster list | grep -q "^$(K3D_CLUSTER_NAME) "; then \
		printf "$(CHECK) Cluster '$(K3D_CLUSTER_NAME)' already exists, skipping creation.\n"; \
	else \
		$(call run_spinner,Creating k3d cluster '$(K3D_CLUSTER_NAME)'...,\
			k3d cluster create --config $(K3D_CONFIG)) && \
		printf "$(CHECK) Cluster '$(K3D_CLUSTER_NAME)' created.\n"; \
	fi
	@kubectl config use-context k3d-$(K3D_CLUSTER_NAME) >/dev/null
	@printf "$(CHECK) Context switched to k3d-$(K3D_CLUSTER_NAME).\n"
	@$(call run_spinner,Waiting for nodes...,\
			kubectl wait --for=condition=ready nodes --all --timeout=60s)
	@printf "$(CHECK) $(BOLD)Cluster nodes ready.$(RESET)\n"
	@echo

	@# a helm killed mid-run (Ctrl+C, Docker restart) leaves its release locked in pending-*; a first install has nothing to roll back to
	@helm list -A --pending --no-headers | while read -r name ns rev _; do \
		[ -n "$$name" ] || continue; \
		printf "$(DROP) Releasing Helm lock on '%s' left by an interrupted run.\n" "$$name"; \
		if [ "$$rev" = 1 ]; then helm uninstall "$$name" -n "$$ns" --wait >/dev/null; \
		else helm rollback "$$name" -n "$$ns" --wait >/dev/null; fi || exit 1; \
	done

	@$(call run_spinner,Installing ingress-nginx...,\
			helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx --force-update && \
			helm repo update && \
			helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
				 --version 4.15.1 \
				 --namespace ingress-nginx --create-namespace \
				 --set controller.service.type=LoadBalancer \
				 --wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Ingress-nginx installed and ready.$(RESET)\n"
	@echo

	@$(MAKE) --no-print-directory deploy-kubo
	@$(MAKE) --no-print-directory deploy-lgtm-stack
	@$(MAKE) --no-print-directory deploy-backend
	@$(MAKE) --no-print-directory deploy-frontend

deploy-kubo:
	@$(call run_spinner,Deploying Kubo (IPFS)...,\
			kubectl apply -f k8s/kubo.yaml && \
			kubectl rollout status deployment/kubo --timeout=120s)
	@printf "$(CHECK) $(BOLD)Kubo (IPFS) deployed and ready.$(RESET)\n"
	@echo

deploy-lgtm-stack:
	@$(call run_spinner,Deploying Loki...,\
			helm upgrade --install loki grafana/loki \
				--version 7.5.0 \
				--namespace default \
				--values k8s/helm/values-loki.yaml \
				--wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Loki deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Tempo...,\
			helm upgrade --install tempo grafana-community/tempo \
				--version 3.2.1 \
				--namespace default \
				--values k8s/helm/values-tempo.yaml \
				--wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Tempo deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Mimir...,\
			helm upgrade --install mimir grafana/mimir-distributed \
				--version 6.2.1 \
				--namespace default \
				--values k8s/helm/values-mimir.yaml \
				--wait --timeout 300s)
	@printf "$(CHECK) $(BOLD)Mimir deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying OpenTelemetry Collector...,\
			helm upgrade --install otel-collector open-telemetry/opentelemetry-collector \
				--version 0.177.0 \
				--namespace default \
				--values k8s/helm/values-otel-collector.yaml \
				--wait --timeout 120s)
	@printf "$(CHECK) $(BOLD)OpenTelemetry Collector deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying OpenTelemetry node agents...,\
			helm upgrade --install otel-agent open-telemetry/opentelemetry-collector \
				--version 0.177.0 \
				--namespace default \
				--values k8s/helm/values-otel-agent.yaml \
				--wait --timeout 120s)
	@printf "$(CHECK) $(BOLD)OpenTelemetry node agents deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Pyroscope...,\
			helm upgrade --install pyroscope grafana/pyroscope \
				--version 2.4.1 \
				--namespace default \
				--values k8s/helm/values-pyroscope.yaml \
				--wait --timeout 180s)
	@printf "$(CHECK) $(BOLD)Pyroscope deployed and ready.$(RESET)\n"
	@echo
	@$(call run_spinner,Applying Grafana datasources and dashboards...,\
			kubectl apply -n default -f k8s/grafana-datasources.yaml && \
			kubectl create configmap grafana-dashboard-metrics -n default \
				--from-file=dashboards/dashboard-metrics.json \
				--dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml | kubectl apply -n default -f - && \
			kubectl create configmap grafana-dashboard-traces -n default \
				--from-file=dashboards/dashboard-traces.json \
				--dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml | kubectl apply -n default -f - && \
			kubectl create configmap grafana-dashboard-cluster -n default \
				--from-file=dashboards/dashboard-cluster.json \
				--dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml | kubectl apply -n default -f - && \
			kubectl create configmap grafana-dashboard-profiles -n default \
				--from-file=dashboards/dashboard-profiles.json \
				--dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml | kubectl apply -n default -f -)
	@printf "$(CHECK) $(BOLD)Grafana datasources and dashboards applied.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying Grafana...,\
			helm upgrade --install grafana grafana/grafana \
				--version 10.5.15 \
				--namespace default \
				--values k8s/helm/values-grafana.yaml \
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
			kubectl apply -f k8s/backend.yaml && \
			kubectl rollout restart deployment/backend && \
			kubectl rollout status deployment/backend --timeout=60s)
	@printf "$(CHECK) $(BOLD)Backend deployed and ready.$(RESET)\n"
	@$(PRUNE_IMAGES)
	@echo

deploy-frontend:
	@$(call run_spinner,Building frontend image...,\
			docker build -t ft-lgtm-frontend:dev frontend/ && \
			k3d image import ft-lgtm-frontend:dev -c $(K3D_CLUSTER_NAME))
	@printf "$(CHECK) $(BOLD)Frontend image built and imported.$(RESET)\n"
	@echo
	@$(call run_spinner,Deploying frontend and lgtm.local ingress...,\
			kubectl apply -f k8s/frontend.yaml -f k8s/ingress.yaml && \
			kubectl rollout restart deployment/frontend && \
			kubectl rollout status deployment/frontend --timeout=60s)
	@printf "$(CHECK) $(BOLD)Frontend deployed and ready.$(RESET)\n"
	@$(PRUNE_IMAGES)
	@echo

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
