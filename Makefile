.PHONY: lint test-unit build secret-scan ci-checks smoke security-smoke test install-hooks help

help:
	@echo "Targets:"
	@echo "  lint            gofmt + go vet on lambda/"
	@echo "  test-unit       go test ./... in lambda/"
	@echo "  build           build both lambda zips (delegates to lambda/Makefile)"
	@echo "  secret-scan     gitleaks scan of the working tree"
	@echo "  ci-checks       lint + test-unit + build + secret-scan (run by pre-commit hook and CI)"
	@echo "  smoke           run tests/smoke.sh against deployed dev"
	@echo "  security-smoke  run tests/security-smoke.sh (EICAR upload + remediation check)"
	@echo "  test            smoke + security-smoke (E2E against deployed dev)"
	@echo "  install-hooks   symlink scripts/pre-commit.sh into .git/hooks/pre-commit"

lint:
	@output=$$(gofmt -l lambda/); \
	if [ -n "$$output" ]; then \
		echo "gofmt: files needing formatting:"; echo "$$output"; exit 1; \
	fi
	cd lambda && go vet ./...

test-unit:
	cd lambda && go test ./...

build:
	$(MAKE) -C lambda build-all

secret-scan:
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo "gitleaks not installed. Install with: brew install gitleaks"; exit 1; \
	}
	gitleaks detect --source . --no-banner --redact

ci-checks: lint test-unit build secret-scan

smoke:
	bash tests/smoke.sh

security-smoke:
	bash tests/security-smoke.sh

test: smoke security-smoke

install-hooks:
	@mkdir -p .git/hooks
	ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
	chmod +x scripts/pre-commit.sh
	@echo "pre-commit hook installed"
