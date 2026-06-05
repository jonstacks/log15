force: ;

CONTRIBUTORS: force
	go run github.com/kevinburke/write_mailmap > CONTRIBUTORS

GO ?= go
COVERAGE_DIR ?= coverage

.PHONY: coverage
coverage:
	mkdir -p $(COVERAGE_DIR)
	$(GO) test -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/root.out ./...
	$(GO) tool cover -func=$(COVERAGE_DIR)/root.out | tee $(COVERAGE_DIR)/root.txt
	cd v3 && $(GO) test -coverpkg=./... -coverprofile=../$(COVERAGE_DIR)/v3.out ./...
	cd v3 && $(GO) tool cover -func=../$(COVERAGE_DIR)/v3.out | tee ../$(COVERAGE_DIR)/v3.txt
	{ \
		echo "root"; \
		cat $(COVERAGE_DIR)/root.txt; \
		echo; \
		echo "v3"; \
		cat $(COVERAGE_DIR)/v3.txt; \
	} | tee $(COVERAGE_DIR)/summary.txt
