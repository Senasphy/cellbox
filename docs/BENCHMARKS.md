# Benchmarks

Session creation has not been benchmarked on the specification's reference machine. Do not treat a local compile, stub runtime, or CI result as a latency measurement.

`make bench` compares session creation against two running instances: a pool-disabled service on port 8081 and a pool-enabled service on port 8080 by default. Set `CELLBOX_COLD_KEY` and `CELLBOX_WARM_KEY` to administrator keys for the respective services. Both services must use the same image and comparable configuration; the script primes refill if needed, waits for warm readiness, and checks the warm creation metric after every sample so cold requests are not mislabeled as warm.

The command prints the host kernel, CPU count/quota, detected memory/cgroup limit, and raw samples with p50/p95 values. Record the output here only after running it on the target host. The phase acceptance target is warm p50 below 60 ms; cold p50 below 400 ms, or a measured result with an explanation when it misses.

## Current measurements

Not measured. This work session could not start the inactive Docker service, and no native OCI image cache was available for a live run.
