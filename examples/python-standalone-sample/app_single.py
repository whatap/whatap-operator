#!/usr/bin/env python3
"""
Single-transaction sample application for Whatap Python Agent (standalone mode).
This script performs a few simple CPU-bound and IO-bound tasks so you can
see activity on the APM dashboard when run with the agent enabled.

Usage (local, if you have whatap_python installed and configured):
  whatap-start-agent app_single.py

Docker usage: see README.md in this folder.
"""
import logging
import math
import os
import random
import sys
import time


def cpu_heavy(n: int) -> int:
    # Simulate CPU work: count primes up to n (naive)
    def is_prime(x: int) -> bool:
        if x < 2:
            return False
        if x % 2 == 0:
            return x == 2
        r = int(math.sqrt(x)) + 1
        for i in range(3, r, 2):
            if x % i == 0:
                return False
        return True

    cnt = 0
    for v in range(2, n + 1):
        if is_prime(v):
            cnt += 1
    return cnt


def io_sleep(min_ms=50, max_ms=200):
    # Simulate I/O wait
    ms = random.randint(min_ms, max_ms)
    time.sleep(ms / 1000.0)
    return ms


def configure_logger() -> logging.Logger:
    level_name = os.getenv("LOG_LEVEL", "INFO").upper()
    level = getattr(logging, level_name, logging.INFO)
    logging.basicConfig(
        level=level,
        format="%(asctime)s %(levelname)s [sample] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    return logging.getLogger("sample")


def main():
    logger = configure_logger()
    try:
        logger.info("single-transaction script started")
        # Read an optional size from argv
        n = 5000
        if len(sys.argv) > 1:
            try:
                n = int(sys.argv[1])
            except ValueError:
                logger.warning("invalid argument for n; using default n=5000")

        # A few rounds of mixed work
        total_primes = 0
        total_slept = 0
        for round_idx in range(5):
            slept = io_sleep()
            total_slept += slept
            primes = cpu_heavy(n)
            total_primes += primes
            logger.info(
                "round=%d slept_ms=%d primes_up_to_%d=%d",
                round_idx,
                slept,
                n,
                primes,
            )

        logger.info(
            "done total_primes=%d total_slept_ms=%d",
            total_primes,
            total_slept,
        )
        logger.info("exiting")
    except Exception:
        # Ensure unexpected errors are logged with stacktrace
        logger.exception("unhandled exception in main")
        raise


if __name__ == "__main__":
    main()
