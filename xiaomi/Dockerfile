FROM python:3.15.2-alpine AS build
RUN apk add --update --no-cache gcc musl-dev libffi-dev openssl-dev
COPY . /app/
WORKDIR /app
# --prefix keeps the copy below independent of the Python version.
# netifaces (via python-miio) is unmaintained and fails on GCC 14+ where
# -Wint-conversion is an error by default; keep it a warning as before.
RUN CFLAGS="-Wno-error=int-conversion" pip install --no-cache-dir --prefix=/install .

FROM python:3.15.2-alpine
COPY --from=build /install /usr/local
CMD app --ip $IP --port 8000 --token $TOKEN
