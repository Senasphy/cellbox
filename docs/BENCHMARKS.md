# Benchmarks

Session creation has not been benchmarked on the specification's reference machine. Do not treat this local measurement as evidence that the reference-machine target is met.

`make bench` compares session creation against two running instances: a pool-disabled service on port 8081 and a pool-enabled service on port 8080 by default. Set `CELLBOX_COLD_KEY` and `CELLBOX_WARM_KEY` to administrator keys for the respective services. Both services must use the same image and comparable configuration; the script primes refill if needed, waits for warm readiness, and checks the warm creation metric after every sample so cold requests are not mislabeled as warm.

The command prints the host kernel, CPU count/quota, detected memory/cgroup limit, and raw samples with p50/p95 values. Record the output here only after running it on the target host. The phase acceptance target is warm p50 below 60 ms; cold p50 below 400 ms, or a measured result with an explanation when it misses.

## Current measurements

Measured 2026-11-23 on the local Docker backend with `alpine:latest`, 10 sequential samples per path. The pool-enabled service's warm path was confirmed by the `cellbox_session_create_seconds_count{source="warm"}` metric after each request.

| Path | p50 | p95 | Target |
| --- | ---: | ---: | ---: |
| Cold | 813 ms | 832 ms | under 400 ms |
| Warm | 374 ms | 487 ms | under 60 ms |

The host reported Linux kernel `7.3.4-arch1-2`, 8 CPUs, and 15,828 MiB RAM, with no cgroup CPU or memory cap detected. Both targets were missed on this host. This is an initial local baseline, not a reference-machine result; the warm pool is functioning, but the measured end-to-end creation path still needs optimization before latency acceptance.
