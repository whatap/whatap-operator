#!/usr/bin/env python3
"""
Multiple-transaction sample application for Whatap Python Agent (standalone mode).
This script exposes several functions and a class method so you can target them
with standalone_transaction_patterns.

Examples:
  STANDALONE_ENABLED=true \
  STANDALONE_TYPE=multiple-transaction \
  STANDALONE_TRANSACTION_PATTERNS="__main__:task_alpha,__main__:Worker.run" \
  whatap-start-agent app_multi.py

Docker usage: see README.md in this folder.
"""
import logging
import os
import random
import time
from typing import List


def configure_logger() -> logging.Logger:
    level_name = os.getenv("LOG_LEVEL", "INFO").upper()
    level = getattr(logging, level_name, logging.INFO)
    logging.basicConfig(
        level=level,
        format="%(asctime)s %(levelname)s [sample] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    return logging.getLogger("sample")


def task_alpha(n: int = 3) -> int:
    """Simulate a task with variable sleep."""
    total = 0
    for _ in range(n):
        ms = random.randint(30, 120)
        time.sleep(ms / 1000.0)
        total += ms
    return total


def task_beta(items: List[int]) -> int:
    """Perform a simple transformation over a list."""
    s = 0
    for x in items:
        s += (x * 3) % 7
    # Slow down a bit
    time.sleep(0.05)
    return s


class Worker:
    def __init__(self, name: str):
        self.name = name

    def run(self, rounds: int = 3) -> int:
        """Run several iterations combining both tasks."""
        acc = 0
        for i in range(rounds):
            acc += task_alpha(2)
            acc += task_beta([i, i + 1, i + 2])
        return acc


def main():
    logger = configure_logger()
    try:
        logger.info("multiple-transaction script started")
        w = Worker("w1")
        total = 0
        for i in range(5):
            ta = task_alpha(2)
            tb = task_beta([i, i + 2, i + 4])
            wr = w.run(2)
            total += (ta + tb + wr)
            logger.info(
                "loop=%d totals ta=%d tb=%d wr=%d acc=%d",
                i,
                ta,
                tb,
                wr,
                total,
            )

        logger.info("done total=%d", total)
        logger.info("exiting")
    except Exception:
        logger.exception("unhandled exception in main")
        raise


if __name__ == "__main__":
    main()
