import prometheus_client
import logging
import time
import argparse
from miio import AirPurifier
from prometheus_client import Gauge

logging.basicConfig()
log = logging.getLogger()
log.setLevel(logging.INFO)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--ip', help='IP', required=True)
    parser.add_argument('--token', help='token', required=True)
    parser.add_argument('--port', help='prometheus port', required=True)
    args = parser.parse_args()

    device = AirPurifier(ip=args.ip, token=args.token)

    volume = Gauge('volume', 'Volume')
    temperature = Gauge('temperature_celsius', 'Celsius')
    humidity = Gauge('humidity_percent', 'Percent')
    aqi = Gauge('aqi_index', 'Index')
    avarageaqi = Gauge('avarage_aqi_index', 'Index')
    filter_hours_used = Gauge("filter_hours_used","filter_hours_used")

    prometheus_client.start_http_server(int(args.port))
    log.info("Server started at port {}".format(args.port))

    while True:
        try:
            status = device.status()
        except Exception as err:
            log.error("Can't get information from device, check token or network: %s", err)
            time.sleep(5)
            continue
        temperature.set(status.temperature)
        humidity.set(status.humidity)
        aqi.set(status.aqi)
        avarageaqi.set(status.average_aqi)
        volume.set(status.volume)
        filter_hours_used.set(status.filter_hours_used)
        time.sleep(15)

if __name__ == '__main__':
    main()
