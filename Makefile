BENCH_DIR := bench
MEM_PROFILE := $(BENCH_DIR)/mem.pb.gz
CPU_PROFILE := $(BENCH_DIR)/cpu.pb.gz
BLOCK_PROFILE := $(BENCH_DIR)/block.pb.gz
MUTEX_PROFILE := $(BENCH_DIR)/mutex.pb.gz
TANGO_PKG := github.com/alesr/tango

.PHONY: test-unit
test-unit:
	go test -v -race -count=1 -short ./...

.PHONY: test-load
test-load:
	go test -v -race -count=1 ./loadtest/...

.PHONY: bench
bench:
	@mkdir -p $(BENCH_DIR)
	go test -run=^$$ -bench=BenchmarkMatchmaking -benchmem \
		-memprofile=$(MEM_PROFILE) \
		-cpuprofile=$(CPU_PROFILE) \
		-blockprofile=$(BLOCK_PROFILE) \
		-mutexprofile=$(MUTEX_PROFILE) \
		$(TANGO_PKG)

# Terminal output profiles
.PHONY: pprof-mem
pprof-mem:
	go tool pprof --alloc_objects -top $(MEM_PROFILE)

.PHONY: pprof-cpu
pprof-cpu:
	go tool pprof -top $(CPU_PROFILE)

.PHONY: pprof-block
pprof-block:
	go tool pprof -top $(BLOCK_PROFILE)

.PHONY: pprof-mutex
pprof-mutex:
	go tool pprof -top $(MUTEX_PROFILE)

# Web interface profiles
.PHONY: pprof-web-mem
pprof-web-mem:
	go tool pprof --http=:8080 $(MEM_PROFILE)

.PHONY: pprof-web-cpu
pprof-web-cpu:
	go tool pprof --http=:8080 $(CPU_PROFILE)

.PHONY: pprof-web-block
pprof-web-block:
	go tool pprof --http=:8080 $(BLOCK_PROFILE)

.PHONY: pprof-web-mutex
pprof-web-mutex:
	go tool pprof --http=:8080 $(MUTEX_PROFILE)

# Combined targets
.PHONY: pprof
pprof: pprof-mem pprof-cpu pprof-block pprof-mutex

.PHONY: pprof-web
pprof-web: pprof-web-mem pprof-web-cpu pprof-web-block pprof-web-mutex
