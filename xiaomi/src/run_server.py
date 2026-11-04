import argparse
import logging
import os
import threading
import time
from dataclasses import dataclass
from socketserver import ThreadingMixIn
from wsgiref.simple_server import WSGIRequestHandler, WSGIServer, make_server

from prometheus_client import REGISTRY, CollectorRegistry, Gauge, make_wsgi_app

logging.basicConfig()
log = logging.getLogger()
log.setLevel(logging.INFO)

POLL_INTERVAL = 15  # seconds between successful reads
RETRY_INTERVAL = 5  # seconds to wait after a failed read


def env_arg(parser, flag, env, help):
    """CLI flag that falls back to an environment variable."""
    default = os.environ.get(env)
    parser.add_argument(flag, help=f'{help} (env: {env})', default=default, required=default is None)


def build_parser():
    parser = argparse.ArgumentParser()
    env_arg(parser, '--ip', 'IP', 'IP')
    env_arg(parser, '--token', 'TOKEN', 'token')
    env_arg(parser, '--port', 'PORT', 'prometheus port')
    return parser


@dataclass
class Metrics:
    volume: Gauge
    temperature: Gauge
    humidity: Gauge
    aqi: Gauge
    avarageaqi: Gauge
    filter_hours_used: Gauge


def create_metrics(registry: CollectorRegistry = REGISTRY) -> Metrics:
    return Metrics(
        volume=Gauge('volume', 'Volume', registry=registry),
        temperature=Gauge('temperature_celsius', 'Celsius', registry=registry),
        humidity=Gauge('humidity_percent', 'Percent', registry=registry),
        aqi=Gauge('aqi_index', 'Index', registry=registry),
        avarageaqi=Gauge('avarage_aqi_index', 'Index', registry=registry),
        filter_hours_used=Gauge('filter_hours_used', 'filter_hours_used', registry=registry),
    )


def update_metrics(metrics: Metrics, status) -> None:
    metrics.temperature.set(status.temperature)
    metrics.humidity.set(status.humidity)
    metrics.aqi.set(status.aqi)
    metrics.avarageaqi.set(status.average_aqi)
    metrics.volume.set(status.volume)
    metrics.filter_hours_used.set(status.filter_hours_used)


def make_app(registry: CollectorRegistry = REGISTRY):
    """WSGI app: /metrics for Prometheus, / redirects there, anything else 404."""
    metrics_app = make_wsgi_app(registry)

    def app(environ, start_response):
        path = environ.get('PATH_INFO') or '/'
        if path == '/metrics':
            return metrics_app(environ, start_response)
        if path == '/':
            start_response('302 Found', [('Location', '/metrics'), ('Content-Length', '0')])
            return [b'']
        body = b'Not Found\n'
        start_response('404 Not Found', [('Content-Type', 'text/plain'), ('Content-Length', str(len(body)))])
        return [body]

    return app


class _ThreadingWSGIServer(ThreadingMixIn, WSGIServer):
    daemon_threads = True  # request threads never keep the process alive


class _QuietHandler(WSGIRequestHandler):
    def log_message(self, format, *args):  # no access log line per scrape
        pass


def start_server(port: int, addr: str = '0.0.0.0', registry: CollectorRegistry = REGISTRY):
    """Starts the metrics HTTP server on a daemon thread; returns the server."""
    httpd = make_server(addr, port, make_app(registry), _ThreadingWSGIServer, handler_class=_QuietHandler)
    threading.Thread(target=httpd.serve_forever, name='metrics-http', daemon=True).start()
    return httpd


def poll_once(device, metrics: Metrics) -> bool:
    """Reads the device once and updates the gauges. Returns False (and
    leaves the previous values in place) when the device can't be read."""
    try:
        status = device.status()
    except Exception as err:
        log.error("Can't get information from device, check token or network: %s", err)
        return False
    update_metrics(metrics, status)
    return True


def run_forever(device, metrics: Metrics, sleep=time.sleep, should_stop=lambda: False) -> None:
    while not should_stop():
        ok = poll_once(device, metrics)
        sleep(POLL_INTERVAL if ok else RETRY_INTERVAL)


def main(argv=None):
    args = build_parser().parse_args(argv)

    from miio import AirPurifier  # imported here so tests don't need python-miio

    device = AirPurifier(ip=args.ip, token=args.token)
    metrics = create_metrics()

    start_server(int(args.port))
    log.info("Server started at port {}".format(args.port))

    run_forever(device, metrics)


if __name__ == '__main__':
    main()
