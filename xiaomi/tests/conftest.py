from dataclasses import dataclass

import pytest
from prometheus_client import CollectorRegistry

import run_server


@dataclass
class FakeStatus:
    temperature: float = 22.5
    humidity: int = 45
    aqi: int = 12
    average_aqi: int = 10
    volume: int = 50
    filter_hours_used: int = 1234


class FakeDevice:
    """Stands in for miio.AirPurifier: returns `status` or raises `error`."""

    def __init__(self, status=None, error=None):
        self.status_value = status or FakeStatus()
        self.error = error
        self.calls = 0

    def status(self):
        self.calls += 1
        if self.error is not None:
            # A fresh exception per call, like a real network error. Re-raising
            # one shared instance would keep extending its __traceback__ and
            # make the *test* leak.
            raise type(self.error)(*self.error.args)
        return self.status_value


@pytest.fixture
def registry():
    return CollectorRegistry()


@pytest.fixture
def metrics(registry):
    return run_server.create_metrics(registry)
