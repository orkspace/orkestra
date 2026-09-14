.PHONY: all cluster-01 cluster-02 cluster-03 cluster-04 cluster-05 cluster-06 \
        validate-all validate-01 validate-02 validate-03 validate-04 validate-05 validate-06 \
        full-all full-01 full-02 full-03 full-04 full-05 full-06 \
        restart restart-01 restart-02 restart-03 restart-04 restart-05 restart-06 \
        cc reload \
        clean clean-01 clean-02 clean-03 clean-04 clean-05 clean-06

CHART     := ../../charts/orkestra
REPO_ROOT := ../..
KIND_CLUSTER      := orkestra-playground
ORK_BASE_IMAGE    := ghcr.io/orkspace/orkestra
ORK_CC_BASE_IMAGE := ghcr.io/orkspace/orkestra-cc
ORK_GW_BASE_IMAGE := ghcr.io/orkspace/orkestra-gateway
NS_01 := orkestra-system-01
NS_02 := orkestra-system-02
NS_03 := orkestra-system-03
NS_04 := orkestra-system-04
NS_05 := orkestra-system-05
NS_06 := orkestra-system-06

# ── Deploy ────────────────────────────────────────────────────────────────────

all: cluster-01 cluster-02 cluster-03 cluster-04 cluster-05 cluster-06

cluster-01:
	@echo "→ cluster-01: hello-website ($(NS_01))"
	cd cluster-01 && ork generate bundle --for runtime,cc -n $(NS_01) -o - | kubectl apply -f -
	kubectl apply -f cluster-01/crd.yaml
	helm upgrade --install orkestra $(CHART) \
		--namespace $(NS_01) \
		--values values.yaml
	kubectl create namespace demo-01 --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f cluster-01/cr.yaml

cluster-02:
	@echo "→ cluster-02: website-with-serviceaccount ($(NS_02))"
	cd cluster-02 && ork generate bundle --for runtime -n $(NS_02) -o - | kubectl apply -f -
	kubectl apply -f cluster-02/crd.yaml
	helm upgrade --install orkestra $(CHART) \
		--namespace $(NS_02) \
		--values values.yaml \
		--set controlCenter.enabled=false
	kubectl create namespace demo-02 --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f cluster-02/cr.yaml

cluster-03:
	@echo "→ cluster-03: secret-distribution ($(NS_03))"
	cd cluster-03 && ork generate bundle --for runtime -n $(NS_03) -o - | kubectl apply -f -
	kubectl apply -f cluster-03/crd.yaml
	kubectl apply -f cluster-03/setup.yaml
	helm upgrade --install orkestra $(CHART) \
		--namespace $(NS_03) \
		--values values.yaml \
		--set controlCenter.enabled=false
	kubectl apply -f cluster-03/cr.yaml

cluster-04:
	@echo "→ cluster-04: app-platform ($(NS_04))"
	cd cluster-04 && ork generate bundle --for runtime -n $(NS_04) -o - | kubectl apply -f -
	kubectl apply -f cluster-04/crd.yaml
	helm upgrade --install orkestra $(CHART) \
		--namespace $(NS_04) \
		--values values.yaml \
		--set controlCenter.enabled=false
	kubectl create namespace demo-04 --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f cluster-04/cr.yaml

cluster-05:
	@echo "→ cluster-05: data-platform ($(NS_05))"
	cd cluster-05 && ork generate bundle --for runtime -n $(NS_05) -o - | kubectl apply -f -
	kubectl apply -f cluster-05/crd.yaml
	helm upgrade --install orkestra $(CHART) \
		--namespace $(NS_05) \
		--values values.yaml \
		--set controlCenter.enabled=false
	kubectl create namespace demo-05 --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f cluster-05/cr.yaml

cluster-06:
	@echo "→ cluster-06: network-suite ($(NS_06))"
	cd cluster-06 && ork generate bundle --for runtime -n $(NS_06) -o - | kubectl apply -f -
	kubectl apply -f cluster-06/crd.yaml
	helm upgrade --install orkestra $(CHART) \
		--namespace $(NS_06) \
		--values values.yaml \
		--set controlCenter.enabled=false
	kubectl create namespace demo-06 --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f cluster-06/cr.yaml

# ── Validate ──────────────────────────────────────────────────────────────────

validate-all: validate-01 validate-02 validate-03 validate-04 validate-05 validate-06

validate-01:
	@echo "→ validate cluster-01"
	cd cluster-01 && ork validate

validate-02:
	@echo "→ validate cluster-02"
	cd cluster-02 && ork validate

validate-03:
	@echo "→ validate cluster-03"
	cd cluster-03 && ork validate

validate-04:
	@echo "→ validate cluster-04"
	cd cluster-04 && ork validate

validate-05:
	@echo "→ validate cluster-05"
	cd cluster-05 && ork validate

validate-06:
	@echo "→ validate cluster-06"
	cd cluster-06 && ork validate

# ── Validate --full ───────────────────────────────────────────────────────────

full-all: full-01 full-02 full-03 full-04 full-05 full-06

full-01:
	@echo "→ validate --full cluster-01"
	cd cluster-01 && ork validate --full

full-02:
	@echo "→ validate --full cluster-02"
	cd cluster-02 && ork validate --full

full-03:
	@echo "→ validate --full cluster-03"
	cd cluster-03 && ork validate --full

full-04:
	@echo "→ validate --full cluster-04"
	cd cluster-04 && ork validate --full

full-05:
	@echo "→ validate --full cluster-05"
	cd cluster-05 && ork validate --full

full-06:
	@echo "→ validate --full cluster-06"
	cd cluster-06 && ork validate --full

# ── Rollout Restart ───────────────────────────────────────────────────────────

restart: restart-01 restart-02 restart-03 restart-04 restart-05 restart-06

restart-01:
	@echo "→ restart cluster-01 ($(NS_01))"
	kubectl rollout restart deployment -n $(NS_01)

restart-02:
	@echo "→ restart cluster-02 ($(NS_02))"
	kubectl rollout restart deployment -n $(NS_02)

restart-03:
	@echo "→ restart cluster-03 ($(NS_03))"
	kubectl rollout restart deployment -n $(NS_03)

restart-04:
	@echo "→ restart cluster-04 ($(NS_04))"
	kubectl rollout restart deployment -n $(NS_04)

restart-05:
	@echo "→ restart cluster-05 ($(NS_05))"
	kubectl rollout restart deployment -n $(NS_05)

restart-06:
	@echo "→ restart cluster-06 ($(NS_06))"
	kubectl rollout restart deployment -n $(NS_06)

# ── Control Center ────────────────────────────────────────────────────────────

cc:
	kubectl port-forward svc/orkestra-cc -n $(NS_01) 8081:8081

# ── Dev Reload (kind only) ────────────────────────────────────────────────────
# Builds runtime + gateway + CC images from source, loads into the kind cluster,
# updates values.yaml with the new dev tag, and re-deploys all clusters via make all.

reload:
	@TAG=dev-$$(date +%s); \
	echo "→ Building images: $$TAG"; \
	$(MAKE) -C $(REPO_ROOT) docker ORK_IMAGE=$(ORK_BASE_IMAGE):$$TAG && \
	$(MAKE) -C $(REPO_ROOT) docker-cc ORK_CC_IMAGE=$(ORK_CC_BASE_IMAGE):$$TAG && \
	echo "→ Loading into kind cluster: $(KIND_CLUSTER)" && \
	kind load docker-image $(ORK_BASE_IMAGE):$$TAG --name $(KIND_CLUSTER) && \
	kind load docker-image $(ORK_CC_BASE_IMAGE):$$TAG --name $(KIND_CLUSTER) && \
	echo "→ Updating values.yaml tag → $$TAG" && \
	sed -i "s/tag: \".*\"/tag: \"$$TAG\"/g" values.yaml && \
	$(MAKE) all

# ── Teardown ──────────────────────────────────────────────────────────────────

clean: clean-01 clean-02 clean-03 clean-04 clean-05 clean-06

clean-01:
	helm uninstall orkestra -n $(NS_01) --ignore-not-found
	kubectl delete -f cluster-01/cr.yaml --ignore-not-found
	kubectl delete -f cluster-01/crd.yaml --ignore-not-found
	kubectl delete namespace demo-01     --ignore-not-found
	cd cluster-01 && ork generate bundle --for runtime,cc -n $(NS_01) -o - | kubectl delete -f - --ignore-not-found

clean-02:
	helm uninstall orkestra -n $(NS_02) --ignore-not-found
	kubectl delete -f cluster-02/cr.yaml --ignore-not-found
	kubectl delete -f cluster-02/crd.yaml --ignore-not-found
	kubectl delete namespace demo-02     --ignore-not-found
	cd cluster-02 && ork generate bundle --for runtime -n $(NS_02) -o - | kubectl delete -f - --ignore-not-found

clean-03:
	helm uninstall orkestra -n $(NS_03) --ignore-not-found
	kubectl delete -f cluster-03/cr.yaml    --ignore-not-found
	kubectl delete -f cluster-03/setup.yaml --ignore-not-found
	kubectl delete -f cluster-03/crd.yaml   --ignore-not-found
	cd cluster-03 && ork generate bundle --for runtime -n $(NS_03) -o - | kubectl delete -f - --ignore-not-found

clean-04:
	helm uninstall orkestra -n $(NS_04) --ignore-not-found
	kubectl delete -f cluster-04/cr.yaml  --ignore-not-found
	kubectl delete -f cluster-04/crd.yaml --ignore-not-found
	kubectl delete namespace demo-04      --ignore-not-found
	cd cluster-04 && ork generate bundle --for runtime -n $(NS_04) -o - | kubectl delete -f - --ignore-not-found

clean-05:
	helm uninstall orkestra -n $(NS_05) --ignore-not-found
	kubectl delete -f cluster-05/cr.yaml  --ignore-not-found
	kubectl delete -f cluster-05/crd.yaml --ignore-not-found
	kubectl delete namespace demo-05      --ignore-not-found
	cd cluster-05 && ork generate bundle --for runtime -n $(NS_05) -o - | kubectl delete -f - --ignore-not-found

clean-06:
	helm uninstall orkestra -n $(NS_06) --ignore-not-found
	kubectl delete -f cluster-06/cr.yaml  --ignore-not-found
	kubectl delete -f cluster-06/crd.yaml --ignore-not-found
	kubectl delete namespace demo-06      --ignore-not-found
	cd cluster-06 && ork generate bundle --for runtime -n $(NS_06) -o - | kubectl delete -f - --ignore-not-found
