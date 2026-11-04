.PHONY: help build lint validate create-repos publish-platforms publish-resource-types publish-bundles clean

# Platforms this catalog needs. Both live in platforms/.
ENABLED_PLATFORMS ?= aws kubernetes

BUNDLES        = $(shell find bundles -mindepth 1 -maxdepth 1 -type d -exec basename {} \;)
TOFU_BUNDLES   = $(shell for b in $(BUNDLES); do [ -d bundles/$$b/src ] && echo $$b; done)
HELM_BUNDLES   = $(shell for b in $(BUNDLES); do [ -d bundles/$$b/chart ] && echo $$b; done)
RESOURCE_TYPES = $(shell find resource-types -mindepth 1 -maxdepth 1 -type d -exec basename {} \;)

help:
	@echo "make build                  - mass bundle build for every bundle"
	@echo "make lint                   - mass bundle lint + helm lint"
	@echo "make validate               - tofu init/validate (OpenTofu bundles) and helm template (Helm bundles)"
	@echo "make publish-platforms      - publish platform credential types ($(ENABLED_PLATFORMS))"
	@echo "make publish-resource-types - publish resource types"
	@echo "make publish-bundles        - publish every bundle to the development channel"

build:
	@for b in $(BUNDLES); do echo "==> build $$b"; mass bundle build --bundle-directory bundles/$$b || exit 1; done

lint: build
	@for b in $(BUNDLES); do echo "==> lint $$b"; mass bundle lint --bundle-directory bundles/$$b || exit 1; done
	@for b in $(HELM_BUNDLES); do echo "==> helm lint $$b"; helm lint bundles/$$b/chart || exit 1; done

validate: build
	@for b in $(TOFU_BUNDLES); do echo "==> tofu validate $$b"; (cd bundles/$$b/src && tofu init -backend=false -input=false >/dev/null && tofu validate) || exit 1; done
	@for b in $(HELM_BUNDLES); do echo "==> helm template $$b"; helm template test bundles/$$b/chart >/dev/null || exit 1; done

# Massdriver v2 publishes into OCI repositories that must exist first. Creation is
# idempotent on a name match; the non-zero exit for an existing repo is ignored.
create-repos:
	@for rt in $(RESOURCE_TYPES); do mass repository create $$rt -t resource-type 2>&1 || true; done
	@for p in $(ENABLED_PLATFORMS); do n=$$(grep -m1 '^name:' platforms/$$p/massdriver.yaml | awk '{print $$2}'); mass repository create $$n -t resource-type 2>&1 || true; done
	@for b in $(BUNDLES); do mass repository create $$b -t bundle 2>&1 || true; done

publish-platforms: create-repos
	@for p in $(ENABLED_PLATFORMS); do echo "==> publish platform $$p"; mass resource-type publish platforms/$$p/massdriver.yaml || exit 1; done

publish-resource-types: create-repos
	@for rt in $(RESOURCE_TYPES); do echo "==> publish resource type $$rt"; mass resource-type publish resource-types/$$rt/massdriver.yaml || exit 1; done

# Development channel only. Promoting a stable release is a deliberate human step:
# run the publish for one bundle by hand without the development flag.
publish-bundles: create-repos build
	@for b in $(BUNDLES); do echo "==> publish $$b (development)"; mass bundle publish --development --bundle-directory bundles/$$b || exit 1; done

clean:
	@find . -name "_massdriver_variables.tf" -delete
	@find . -name "schema-*.json" -delete
	@find . -name ".terraform.lock.hcl" -delete
	@find . -name ".terraform" -type d -exec rm -rf {} + 2>/dev/null || true
