import http.client
import threading

import pytest

import run_server


@pytest.fixture
def server(registry, metrics):
    httpd = run_server.start_server(0, addr='127.0.0.1', registry=registry)
    yield httpd
    httpd.shutdown()
    httpd.server_close()


def get(httpd, path):
    conn = http.client.HTTPConnection('127.0.0.1', httpd.server_port, timeout=5)
    try:
        conn.request('GET', path)
        resp = conn.getresponse()
        return resp.status, resp.getheader('Location'), resp.read()
    finally:
        conn.close()


def test_root_redirects_to_metrics(server):
    status, location, _ = get(server, '/')
    assert status == 302
    assert location == '/metrics'


def test_metrics_served(server, metrics):
    run_server.update_metrics(metrics, __import__('conftest').FakeStatus())
    status, _, body = get(server, '/metrics')
    assert status == 200
    assert b'temperature_celsius 22.5' in body


def test_unknown_path_is_404(server):
    status, _, _ = get(server, '/favicon.ico')
    assert status == 404


@pytest.mark.leak
def test_many_requests_do_not_leave_threads(server):
    before = threading.active_count()
    for _ in range(500):
        get(server, '/metrics')
        get(server, '/')
    # request threads are short-lived daemon threads; give the last one a moment
    for _ in range(50):
        if threading.active_count() <= before:
            break
        threading.Event().wait(0.02)
    assert threading.active_count() <= before
