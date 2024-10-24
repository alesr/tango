.PHONY: bench pprof pprof-mem pprof-cpu pprof-block pprof-mutex pprof-web-mem pprof-web-cpu pprof-web-block pprof-web-mutex

BENCH_DIR := bench
MEM_PROFILE := $(BENCH_DIR)/mem.pb.gz
CPU_PROFILE := $(BENCH_DIR)/cpu.pb.gz
BLOCK_PROFILE := $(BENCH_DIR)/block.pb.gz
MUTEX_PROFILE := $(BENCH_DIR)/mutex.pb.gz

TANGO_PKG := github.com/alesr/tango

bench:
	@mkdir -p $(BENCH_DIR)
	go test -run=^$$ -bench=BenchmarkMatchmaking -benchmem \
		-memprofile=$(MEM_PROFILE) \
		-cpuprofile=$(CPU_PROFILE) \
		-blockprofile=$(BLOCK_PROFILE) \
		-mutexprofile=$(MUTEX_PROFILE) \
		$(TANGO_PKG)

# Terminal output profiles
pprof-mem:
	go tool pprof --alloc_objects -top $(MEM_PROFILE)

pprof-cpu:
	go tool pprof -top $(CPU_PROFILE)

pprof-block:
	go tool pprof -top $(BLOCK_PROFILE)

pprof-mutex:
	go tool pprof -top $(MUTEX_PROFILE)

# Web interface profiles
pprof-web-mem:
	go tool pprof --http=:8080 $(MEM_PROFILE)

pprof-web-cpu:
	go tool pprof --http=:8080 $(CPU_PROFILE)

pprof-web-block:
	go tool pprof --http=:8080 $(BLOCK_PROFILE)

pprof-web-mutex:
	go tool pprof --http=:8080 $(MUTEX_PROFILE)

# Combined targets
pprof: pprof-mem pprof-cpu pprof-block pprof-mutex

pprof-web: pprof-web-mem pprof-web-cpu pprof-web-block pprof-web-mutex
