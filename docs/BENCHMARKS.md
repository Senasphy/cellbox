# Benchmarks

Session creation has not been benchmarked on the specification's reference machine. Do not treat this local measurement as evidence that the reference-machine target is met.

`make bench` compares session creation against two running instances: a pool-disabled service on port 8081 and a pool-enabled service on port 8080 by default. Set `CELLBOX_COLD_KEY` and `CELLBOX_WARM_KEY` to administrator keys for the respective services. Both services must use the same image and comparable configuration; the script primes refill if needed, waits for warm readiness, and checks the warm creation metric after every sample so cold requests are not mislabeled as warm.

The command prints the host kernel, CPU count/quota, detected memory/cgroup limit, and raw samples with p50/p95 values. Record the output here only after running it on the target host. The phase acceptance target is warm p50 below 60 ms; cold p50 below 400 ms, or a measured result with an explanation when it misses.

## Current measurements

Measured 2026-11-23 on the local host with `alpine:latest`, 10 sequential samples per path. The pool-enabled service's warm path was confirmed by the `cellbox_session_create_seconds_count{source="warm"}` metric after each request.

| Backend and revision | Path | p50 | p95 | Target |
| --- | --- | ---: | ---: | ---: |
| Docker, before native cache change | Cold | 813 ms | 832 ms | under 400 ms |
| Docker, before native cache change | Warm | 374 ms | 487 ms | under 60 ms |
| Native, before native cache change | Cold | 2,844 ms | 3,823 ms | under 400 ms |
| Native, before native cache change | Warm | 1,464 ms | 1,746 ms | under 60 ms |
| Native, with resolved-image cache | Cold | 42 ms | 1,955 ms | under 400 ms |
| Native, with resolved-image cache | Warm | 8.6 ms | 9.9 ms | under 60 ms |

The host reported Linux kernel `7.3.4-arch1-2`, 8 CPUs, and 15,828 MiB RAM, with no cgroup CPU or memory cap detected. The native cache change memoizes up to 128 resolved references per service process, avoiding repeated registry manifest lookups while retaining the content-addressed rootfs. On the cached native run, warm p50 met the target and cold p50 met the target; its first sample still fetched the image manifest and accounts for the high p95. Docker remains above both targets in this environment. These are measured local results, not reference-machine results.

## Concurrent load check (`test/load`)

Measured 2026-11-25 on the local host against the native backend (`pool.enabled: false`, `alpine:latest`, concurrency 4, 15s duration) inside a delegated systemd user scope:

- Completed cycles: 272 (`errors: 0`, `leaked_cgroups: 0`)
- Create latency: p50 `95.35 ms`, p95 `151.09 ms`, p99 `188.74 ms`
- Exec latency: p50 `86.81 ms`, p95 `146.37 ms`, p99 `182.19 ms`
- Server process footprint: RSS `26,492 KiB` → `37,368 KiB`, open FDs `84` → `86`

A full 1-hour soak run has not been executed.
