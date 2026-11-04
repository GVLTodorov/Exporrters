"""Memory / resource leak tests (run with `pytest -m leak`).

The exporter is a forever loop, so anything retained per poll - status
objects, exception tracebacks, log records, metric children - grows
without bound. Each test runs thousands of polls and checks the traced
Python heap and thread count stay flat.
"""
import gc
import logging
import threading
import tracemalloc

import pytest

import run_server
from conftest import FakeDevice, FakeStatus

pytestmark = pytest.mark.leak


def heap_growth(fn, warmup=200, cycles=10_000):
    for _ in range(warmup):
        fn()
    gc.collect()
    tracemalloc.start()
    try:
        before = tracemalloc.get_traced_memory()[0]
        for _ in range(cycles):
            fn()
        gc.collect()
        after = tracemalloc.get_traced_memory()[0]
    finally:
        tracemalloc.stop()
    return after - before


@pytest.fixture(autouse=True)
def quiet_logs():
    logging.disable(logging.CRITICAL)  # error path logs on every failed poll
    yield
    logging.disable(logging.NOTSET)


def test_successful_polls_do_not_grow_heap(metrics):
    device = FakeDevice(FakeStatus())
    growth = heap_growth(lambda: run_server.poll_once(device, metrics))
    assert growth < 64 * 1024, f"heap grew {growth} bytes over 10k successful polls"


def test_failing_device_does_not_retain_errors(metrics):
    # Exceptions hold their traceback and frames; keeping a reference to the
    # last error anywhere would pin every frame of every failed poll.
    device = FakeDevice(error=OSError("No response from the device"))
    growth = heap_growth(lambda: run_server.poll_once(device, metrics))
    assert growth < 64 * 1024, f"heap grew {growth} bytes over 10k failed polls"


def test_metric_series_stay_fixed(registry, metrics):
    device = FakeDevice()
    for i in range(5_000):
        device.status_value = FakeStatus(temperature=i % 40, volume=i % 100)
        run_server.poll_once(device, metrics)
    samples = [s for m in registry.collect() for s in m.samples]
    # 6 gauges, one sample each - values change, the series set never grows
    assert len(samples) == 6


def test_run_loop_starts_no_threads(metrics):
    before = threading.active_count()
    n = 0

    def stop():
        nonlocal n
        n += 1
        return n > 2_000

    run_server.run_forever(FakeDevice(), metrics, sleep=lambda s: None, should_stop=stop)
    assert threading.active_count() == before
