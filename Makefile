CONTEXT ?= netclode
NAMESPACE ?= netclode

.PHONY: rollout rollout-control-plane rollout-agent deploy test-ios proto proto-lint proto-breaking proto-setup

# Proto generation
proto: proto-setup ## Generate code from proto files
	@mkdir -p services/control-plane/gen
	@mkdir -p services/agent/gen
	@mkdir -p clients/ios/Netclode/Generated
	cd proto && buf generate

proto-lint: proto-setup ## Lint proto files
	cd proto && buf lint

proto-breaking: proto-setup ## Check for breaking changes against main
	cd proto && buf breaking --against '.git#branch=main'

proto-setup: ## Install buf if not present
	@which buf > /dev/null || (echo "Installing buf..." && brew install bufbuild/buf/buf)

rollout: ## Rollout a deployment: make rollout target=control-plane
ifndef target
	$(error target is required. Usage: make rollout target=control-plane)
endif
	kubectl --context $(CONTEXT) -n $(NAMESPACE) rollout restart deployment/$(target)

rollout-control-plane: ## Rollout control-plane
	kubectl --context $(CONTEXT) -n $(NAMESPACE) rollout restart deployment/control-plane

rollout-agent: ## Rollout agent (drains warm pool to pick up new image)
	@echo "Scaling warm pool to 0..."
	kubectl --context $(CONTEXT) -n $(NAMESPACE) patch sandboxwarmpool netclode-agent-pool -p '{"spec":{"replicas":0}}' --type=merge
	@echo "Waiting for warm pods to terminate..."
	@sleep 5
	@echo "Scaling warm pool back to 1..."
	kubectl --context $(CONTEXT) -n $(NAMESPACE) patch sandboxwarmpool netclode-agent-pool -p '{"spec":{"replicas":1}}' --type=merge
	@echo "Warm pool refreshed with new agent image"

drain-warmpool: ## Drain warm pool to pick up new agent image
	@echo "Scaling warm pool to 0..."
	kubectl --context $(CONTEXT) -n $(NAMESPACE) patch sandboxwarmpool netclode-agent-pool -p '{"spec":{"replicas":0}}' --type=merge
	@echo "Waiting for warm pods to terminate..."
	@sleep 5
	@echo "Scaling warm pool back to 1..."
	kubectl --context $(CONTEXT) -n $(NAMESPACE) patch sandboxwarmpool netclode-agent-pool -p '{"spec":{"replicas":1}}' --type=merge
	@echo "Warm pool refreshed"

deploy: ## Wait for CI then rollout control-plane
	gh run watch $$(gh run list --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status
	$(MAKE) rollout-control-plane

test-ios: ## Run iOS unit tests
	cd clients/ios && xcodebuild test -scheme NetclodeTests -destination 'platform=macOS' -quiet

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
