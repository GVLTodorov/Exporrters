import logging

import pytest

import run_server
from conftest import FakeDevice, FakeStatus


@pytest.fixture(autouse=True)
def clean_env(monkeypatch):
    for name in ("IP", "TOKEN", "PORT"):
        monkeypatch.delenv(name, raising=False)


def test_parser_requires_all_flags_without_env():
    with pytest.raises(SystemExit):
        run_server.build_parser().parse_args([])


def test_parser_reads_flags():
    args = run_server.build_parser().parse_args(["--ip", "1.2.3.4", "--token", "ab", "--port", "8000"])
    assert (args.ip, args.token, args.port) == ("1.2.3.4", "ab", "8000")


def test_parser_falls_back_to_env(monkeypatch):
    monkeypatch.setenv("IP", "10.0.0.5")
    monkeypatch.setenv("TOKEN", "cafe")
    monkeypatch.setenv("PORT", "9000")
    args = run_server.build_parser().parse_args([])
    assert (args.ip, args.token, args.port) == ("10.0.0.5", "cafe", "9000")


def test_flag_overrides_env(monkeypatch):
    monkeypatch.setenv("IP", "10.0.0.5")
    monkeypatch.setenv("TOKEN", "cafe")
    monkeypatch.setenv("PORT", "9000")
    args = run_server.build_parser().parse_args(["--port", "8000"])
    assert args.port == "8000"


def test_metric_names_are_stable(registry, metrics):
    # Grafana dashboards depend on these names.
    names = {m.name for m in registry.collect()}
    assert names == {
        "volume", "temperature_celsius", "humidity_percent",
        "aqi_index", "avarage_aqi_index", "filter_hours_used",
    }


def test_update_metrics(registry, metrics):
    run_server.update_metrics(metrics, FakeStatus())
    get = registry.get_sample_value
    assert get("temperature_celsius") == 22.5
    assert get("humidity_percent") == 45
    assert get("aqi_index") == 12
    assert get("avarage_aqi_index") == 10
    assert get("volume") == 50
    assert get("filter_hours_used") == 1234


def test_poll_once_success(registry, metrics):
    assert run_server.poll_once(FakeDevice(FakeStatus(temperature=30)), metrics) is True
    assert registry.get_sample_value("temperature_celsius") == 30


def test_poll_once_failure_keeps_last_values(registry, metrics, caplog):
    run_server.poll_once(FakeDevice(FakeStatus(humidity=60)), metrics)
    with caplog.at_level(logging.ERROR):
        ok = run_server.poll_once(FakeDevice(error=OSError("timeout")), metrics)
    assert ok is False
    assert registry.get_sample_value("humidity_percent") == 60
    assert "check token or network" in caplog.text


def test_run_forever_sleeps_poll_or_retry_interval(metrics):
    sleeps = []
    device = FakeDevice()
    results = iter([None, OSError("down"), None])

    def status():
        err = next(results)
        if err:
            raise err
        return FakeStatus()

    device.status = status
    run_server.run_forever(device, metrics, sleep=sleeps.append, should_stop=lambda: len(sleeps) == 3)
    assert sleeps == [run_server.POLL_INTERVAL, run_server.RETRY_INTERVAL, run_server.POLL_INTERVAL]
