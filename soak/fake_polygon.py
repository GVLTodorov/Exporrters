"""Minimal fake of Polygon.io's /v2/aggs/ticker/<SYM>/prev for the soak test.

Prices random-walk per symbol. A share of requests misbehave the way the
real free tier does, so the exporter's error, rate-limit and cache-fallback
paths run too: 5% HTTP 429, 2% HTTP 500, 1% slow (3 s) responses.
"""
import json
import random
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PATH = re.compile(r"^/v2/aggs/ticker/([A-Z.]+)/prev$")
prices: dict[str, float] = {}
counts = {"ok": 0, "429": 0, "500": 0, "slow": 0, "404": 0}
lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split("?", 1)[0]
        m = PATH.match(path)
        if not m:
            return self.reply(404, {"status": "NOT_FOUND"}, "404")
        roll = random.random()
        if roll < 0.05:
            return self.reply(429, {"status": "ERROR", "error": "You've exceeded the maximum requests per minute"}, "429")
        if roll < 0.07:
            return self.reply(500, {"status": "ERROR", "error": "internal"}, "500")
        if roll < 0.08:
            time.sleep(3)
            kind = "slow"
        else:
            kind = "ok"
        sym = m.group(1)
        with lock:
            p = prices.get(sym, random.uniform(20, 900))
            p = max(1.0, p * (1 + random.gauss(0, 0.01)))
            prices[sym] = p
        self.reply(200, {"status": "OK", "ticker": sym, "resultsCount": 1, "results": [{"c": round(p, 2)}]}, kind)

    def reply(self, code, payload, kind):
        body = json.dumps(payload).encode()
        with lock:
            counts[kind] += 1
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


def report():
    while True:
        time.sleep(60)
        with lock:
            print("fake-polygon requests:", dict(counts), flush=True)


if __name__ == "__main__":
    threading.Thread(target=report, daemon=True).start()
    print("fake-polygon listening on :8080", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
